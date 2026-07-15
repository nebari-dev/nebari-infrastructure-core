package argocd

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"sigs.k8s.io/yaml"
)

// deriveProjectScopes renders NIC's own embedded app and manifest templates and
// returns the deduplicated, sorted set of source repositories and target
// namespaces they use. This is what scopes the foundational AppProject, so it
// tracks the actual app set automatically. Input is trusted (compiled-in
// templates), never GitOps-repo content.
func deriveProjectScopes(ctx context.Context, data TemplateData) (sourceRepos []string, namespaces []string, err error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.deriveProjectScopes")
	defer span.End()

	repoSet := map[string]struct{}{}
	nsSet := map[string]struct{}{}

	walkErr := fs.WalkDir(templates, filepath.Join(templateDir, "apps"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		base := filepath.Base(path)
		if base == "root.yaml" || strings.HasPrefix(base, "_") {
			return nil // root app-of-apps and examples are not scoped child apps
		}
		return collectFromTemplate(path, data, repoSet, nsSet)
	})
	if walkErr != nil {
		span.RecordError(walkErr)
		return nil, nil, fmt.Errorf("failed walking app templates: %w", walkErr)
	}

	// Manifests referenced by the plain-manifest apps carry their namespaces in
	// the resources themselves (no Application destination.namespace).
	manErr := fs.WalkDir(templates, filepath.Join(templateDir, "manifests"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		return collectFromTemplate(path, data, repoSet, nsSet)
	})
	if manErr != nil {
		span.RecordError(manErr)
		return nil, nil, fmt.Errorf("failed walking manifest templates: %w", manErr)
	}

	delete(repoSet, "")
	delete(repoSet, "*")
	delete(nsSet, "")
	delete(nsSet, "*")

	sourceRepos = mapKeysSorted(repoSet)
	namespaces = mapKeysSorted(nsSet)
	return sourceRepos, namespaces, nil
}

// collectFromTemplate renders one template file and records the source repoURLs
// and namespaces of every YAML document it contains.
func collectFromTemplate(path string, data TemplateData, repoSet, nsSet map[string]struct{}) error {
	content, err := fs.ReadFile(templates, path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	rendered, err := processTemplate(filepath.Base(path), content, data)
	if err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}
	for _, doc := range splitYAMLDocs(string(rendered)) {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var obj projectScopeDoc
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return fmt.Errorf("parse rendered doc in %s: %w", path, err)
		}
		if obj.Metadata.Namespace != "" {
			nsSet[obj.Metadata.Namespace] = struct{}{}
		}
		if obj.Spec.Destination.Namespace != "" {
			nsSet[obj.Spec.Destination.Namespace] = struct{}{}
		}
		if obj.Spec.Source.RepoURL != "" {
			repoSet[obj.Spec.Source.RepoURL] = struct{}{}
		}
		for _, s := range obj.Spec.Sources {
			if s.RepoURL != "" {
				repoSet[s.RepoURL] = struct{}{}
			}
		}
	}
	return nil
}

// splitYAMLDocs splits rendered YAML into documents on lines that are exactly
// "---" (a YAML document separator), avoiding false splits on "---" that appears
// inside a value.
func splitYAMLDocs(s string) []string {
	var docs []string
	var cur []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "---" {
			docs = append(docs, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return append(docs, strings.Join(cur, "\n"))
}

// projectScopeDoc is the minimal shape read from each rendered YAML document.
// Recognized shapes: namespaces come from metadata.namespace and
// spec.destination.namespace; source repos from spec.source.repoURL and
// spec.sources[].repoURL. A template that declares a namespace or repo ONLY via
// a different shape (a deeply-nested field, or a Kustomize top-level namespace:)
// is not seen here and must also be declared via a recognized shape, or the live
// foundational deploy (journey 3) will surface the gap.
type projectScopeDoc struct {
	Metadata struct {
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Destination struct {
			Namespace string `json:"namespace"`
		} `json:"destination"`
		Source struct {
			RepoURL string `json:"repoURL"`
		} `json:"source"`
		Sources []struct {
			RepoURL string `json:"repoURL"`
		} `json:"sources"`
	} `json:"spec"`
}

func mapKeysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// packHelmRepository is the documented source for software-pack Helm charts.
const packHelmRepository = "https://nebari-dev.github.io/helm-repository"

// projectsTemplate renders the three AppProjects. foundational and nebari-apps
// keep wildcard resource whitelists on purpose (kind-level restriction is the
// admission-controller follow-up, #480). default is deny-all so it cannot be a
// project-escape hatch.
const projectsTemplate = `
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: foundational
  namespace: argocd
spec:
  description: Nebari foundational infrastructure services (NIC-owned).
  sourceRepos:
{{- range .SourceRepos }}
    - '{{ . }}'
{{- end }}
  destinations:
{{- range .Namespaces }}
    - namespace: {{ . }}
      server: https://kubernetes.default.svc
{{- end }}
  clusterResourceWhitelist:
    - group: '*'
      kind: '*'
  namespaceResourceWhitelist:
    - group: '*'
      kind: '*'
---
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: nebari-apps
  namespace: argocd
spec:
  description: Software packs (NebariApp-based user applications).
  sourceRepos:
    - '{{ .PackHelmRepository }}'
    - '{{ .GitRepoURL }}'
  destinations:
    - namespace: '*'
      server: https://kubernetes.default.svc
  clusterResourceWhitelist:
    - group: '*'
      kind: '*'
  namespaceResourceWhitelist:
    - group: '*'
      kind: '*'
---
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: default
  namespace: argocd
spec:
  description: Locked down. Use foundational (NIC) or nebari-apps (packs).
  sourceRepos: []
  destinations: []
  clusterResourceWhitelist: []
  namespaceResourceWhitelist: []
`

// RenderProjects returns the foundational, nebari-apps, and default AppProject
// objects, with foundational's scopes derived from the embedded templates.
func RenderProjects(ctx context.Context, data TemplateData) ([]*unstructured.Unstructured, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.RenderProjects")
	defer span.End()

	repos, namespaces, err := deriveProjectScopes(ctx, data)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	tmplData := struct {
		SourceRepos        []string
		Namespaces         []string
		GitRepoURL         string
		PackHelmRepository string
	}{repos, namespaces, data.GitRepoURL, packHelmRepository}

	tmpl, err := template.New("projects").Parse(projectsTemplate)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to parse projects template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tmplData); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to execute projects template: %w", err)
	}

	decoder := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	var objs []*unstructured.Unstructured
	for _, doc := range strings.Split(buf.String(), "\n---") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		obj := &unstructured.Unstructured{}
		if _, _, err := decoder.Decode([]byte(doc), nil, obj); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to decode project manifest: %w", err)
		}
		objs = append(objs, obj)
	}
	return objs, nil
}
