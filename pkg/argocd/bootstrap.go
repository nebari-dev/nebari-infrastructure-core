package argocd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// rootAppOfAppsTemplate is the template for the root App-of-Apps Application
const rootAppOfAppsTemplate = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nebari-root
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: nebari-foundational
    app.kubernetes.io/managed-by: nebari-infrastructure-core
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: foundational
  source:
    repoURL: {{ .GitRepoURL }}
    targetRevision: {{ .GitBranch }}
    path: {{ if .GitPath }}{{ .GitPath }}/{{ end }}apps
    directory:
      recurse: false
      include: '*.yaml'
      exclude: 'root.yaml'
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
    syncOptions:
      - CreateNamespace=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
`

// ApplyRootAppOfApps applies the root App-of-Apps Application which triggers
// ArgoCD to sync all child applications from the git repository or local path.
func ApplyRootAppOfApps(ctx context.Context, kubeconfigBytes []byte, gitConfig *git.Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.ApplyRootAppOfApps")
	defer span.End()

	if gitConfig == nil {
		return fmt.Errorf("git configuration is required for root App-of-Apps")
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Applying root App-of-Apps").
		WithResource("nebari-root").
		WithAction("installing"))

	// Prepare template data
	data := struct {
		GitRepoURL string
		GitBranch  string
		GitPath    string
	}{
		GitRepoURL: gitConfig.URL,
		GitBranch:  gitConfig.GetBranch(),
		GitPath:    gitConfig.Path,
	}

	// Parse and execute template
	tmpl, err := template.New("root-app").Parse(rootAppOfAppsTemplate)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to parse root App-of-Apps template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to execute root App-of-Apps template: %w", err)
	}

	// Parse the rendered manifest
	decoder := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err = decoder.Decode(buf.Bytes(), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode root App-of-Apps manifest: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := NewDynamicClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Apply the root App-of-Apps
	if err := ApplyApplication(ctx, dynamicClient, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply root App-of-Apps: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Root App-of-Apps applied").
		WithResource("nebari-root").
		WithAction("applied").
		WithMetadata("info", "ArgoCD will now sync all applications from git"))

	return nil
}

// InstallProject installs the foundational, nebari-apps, and locked-down default
// ArgoCD AppProjects. foundational is scoped to the repos and namespaces derived
// from NIC's own app templates; nebari-apps is the home for software packs;
// default is deny-all.
func InstallProject(ctx context.Context, kubeconfigBytes []byte, data TemplateData) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.InstallProject")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing ArgoCD AppProjects").
		WithResource("argocd-project").
		WithAction("installing"))

	objs, err := RenderProjects(ctx, data)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to render AppProjects: %w", err)
	}

	dynamicClient, err := NewDynamicClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	for _, obj := range objs {
		if err := applyResource(ctx, dynamicClient, obj); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to apply AppProject %q: %w", obj.GetName(), err)
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "ArgoCD AppProjects installed").
		WithResource("argocd-project").
		WithAction("installed"))
	return nil
}

// applyResource applies a Kubernetes resource using the dynamic client.
// The client parameter allows for dependency injection - use NewDynamicClient for production
// or fake.NewSimpleDynamicClient for tests.
func applyResource(ctx context.Context, client dynamic.Interface, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	resourceName := pluralizeKind(gvk.Kind)
	gvr := gvk.GroupVersion().WithResource(resourceName)
	namespace := obj.GetNamespace()

	// Try to get existing resource
	var existingObj *unstructured.Unstructured
	var err error
	if namespace != "" {
		existingObj, err = client.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		existingObj, err = client.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
	}

	if err == nil {
		// Resource exists, update it
		obj.SetResourceVersion(existingObj.GetResourceVersion())
		if namespace != "" {
			_, err = client.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
		} else {
			_, err = client.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
	} else {
		// Resource doesn't exist, create it
		if namespace != "" {
			_, err = client.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
		} else {
			_, err = client.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to create resource: %w", err)
		}
	}

	return nil
}

// pluralizeKind converts a Kubernetes Kind to its plural resource name
func pluralizeKind(kind string) string {
	lower := strings.ToLower(kind)

	// Special cases for common Kubernetes resources
	switch lower {
	case "gatewayclass":
		return "gatewayclasses"
	case "ingressclass":
		return "ingressclasses"
	case "storageclass":
		return "storageclasses"
	case "priorityclass":
		return "priorityclasses"
	}

	// Handle words ending in 's', 'x', 'z', 'ch', 'sh' -> add 'es'
	if strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "ch") ||
		strings.HasSuffix(lower, "sh") {
		return lower + "es"
	}

	// Handle words ending in 'y' preceded by consonant -> 'ies'
	if strings.HasSuffix(lower, "y") && len(lower) > 1 {
		beforeY := lower[len(lower)-2]
		if beforeY != 'a' && beforeY != 'e' && beforeY != 'i' && beforeY != 'o' && beforeY != 'u' {
			return lower[:len(lower)-1] + "ies"
		}
	}

	// Default: just add 's'
	return lower + "s"
}
