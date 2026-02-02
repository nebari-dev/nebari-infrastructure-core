package argocd

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

//go:embed manifests/argocd-project.yaml
var argoCDProjectManifest string

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
// ArgoCD to sync all child applications from the git repository.
func ApplyRootAppOfApps(ctx context.Context, kubeconfigBytes []byte, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.ApplyRootAppOfApps")
	defer span.End()

	if cfg.GitRepository == nil {
		return fmt.Errorf("git repository configuration is required for root App-of-Apps")
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
		GitRepoURL: cfg.GitRepository.URL,
		GitBranch:  cfg.GitRepository.GetBranch(),
		GitPath:    cfg.GitRepository.Path,
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

	// Apply the root App-of-Apps
	if err := ApplyApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply root App-of-Apps: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Root App-of-Apps applied").
		WithResource("nebari-root").
		WithAction("applied").
		WithMetadata("info", "ArgoCD will now sync all applications from git"))

	return nil
}

// InstallProject installs the ArgoCD AppProject for foundational services
func InstallProject(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.InstallProject")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing ArgoCD Project for foundational services").
		WithResource("argocd-project").
		WithAction("installing"))

	// Parse ArgoCD Project manifest
	decoder := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(argoCDProjectManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode ArgoCD Project manifest: %w", err)
	}

	// Apply ArgoCD Project resource
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply ArgoCD Project: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "ArgoCD Project created").
		WithResource("argocd-project").
		WithAction("created"))

	return nil
}

// applyResource applies a Kubernetes resource using the dynamic client
func applyResource(ctx context.Context, kubeconfigBytes []byte, obj *unstructured.Unstructured) error {
	// Create Kubernetes REST config
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	gvk := obj.GroupVersionKind()
	resourceName := pluralizeKind(gvk.Kind)
	gvr := gvk.GroupVersion().WithResource(resourceName)
	namespace := obj.GetNamespace()

	// Try to get existing resource
	var existingObj *unstructured.Unstructured
	if namespace != "" {
		existingObj, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		existingObj, err = dynamicClient.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
	}

	if err == nil {
		// Resource exists, update it
		obj.SetResourceVersion(existingObj.GetResourceVersion())
		if namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
		} else {
			_, err = dynamicClient.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
	} else {
		// Resource doesn't exist, create it
		if namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
		} else {
			_, err = dynamicClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
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
