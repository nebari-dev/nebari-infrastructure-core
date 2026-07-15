package argocd

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
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
	for _, doc := range strings.Split(string(rendered), "\n---") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var obj projectScopeDoc
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			continue // non-object docs (comments, blank) are skipped
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

// projectScopeDoc is the minimal shape needed to read sources and namespaces
// from both Application manifests and plain resource manifests.
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
