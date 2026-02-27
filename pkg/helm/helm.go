// Package helm provides shared Helm SDK utilities for installing and managing
// Helm charts. It wraps common operations like creating action configurations,
// managing kubeconfig files, and adding Helm repositories.
package helm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// KubeconfigGetter implements the Helm RESTClientGetter interface using a
// kubeconfig file path.
type KubeconfigGetter struct {
	path string
}

func (k *KubeconfigGetter) ToRESTConfig() (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags("", k.path)
}

func (k *KubeconfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	config, err := k.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (k *KubeconfigGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := k.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	return mapper, nil
}

func (k *KubeconfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: k.path},
		&clientcmd.ConfigOverrides{},
	)
}

// NewActionConfig creates a new Helm action configuration using the given
// kubeconfig path and namespace.
func NewActionConfig(kubeconfigPath string, namespace string) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(
		&KubeconfigGetter{path: kubeconfigPath},
		namespace,
		os.Getenv("HELM_DRIVER"), // defaults to "secret" if empty
		func(format string, v ...any) {
			// Helm debug logging (can be customized)
		},
	); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action config: %w", err)
	}

	return actionConfig, nil
}

// AddRepo adds or updates a Helm chart repository with the given name and URL.
func AddRepo(ctx context.Context, name, url string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "helm.AddRepo")
	defer span.End()

	span.SetAttributes(
		attribute.String("repo_name", name),
		attribute.String("repo_url", url),
	)

	settings := cli.New()

	// Create repo entry
	entry := &repo.Entry{
		Name: name,
		URL:  url,
	}

	// Get repo file path
	repoFile := settings.RepositoryConfig

	// Add or update repo
	chartRepo, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create chart repository: %w", err)
	}

	// Download index file
	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to download repository index: %w", err)
	}

	// Load existing repo file
	repoFileObj := repo.NewFile()
	if _, err := os.Stat(repoFile); err == nil {
		repoFileObj, err = repo.LoadFile(repoFile)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to load repository file: %w", err)
		}
	}

	// Add or update entry
	repoFileObj.Update(entry)

	// Save repo file
	if err := repoFileObj.WriteFile(repoFile, 0644); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write repository file: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Added Helm repository %q", name)).
		WithResource("helm-repo").
		WithAction("added").
		WithMetadata("repo_name", name).
		WithMetadata("repo_url", url))

	return nil
}

// WriteTempKubeconfig writes kubeconfig bytes to a temporary file and returns
// the file path, a cleanup function to remove the file, and any error.
func WriteTempKubeconfig(kubeconfigBytes []byte) (string, func(), error) {
	tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp kubeconfig: %w", err)
	}

	tmpPath := filepath.Clean(tmpFile.Name())

	if _, err := tmpFile.Write(kubeconfigBytes); err != nil {
		_ = os.Remove(tmpPath)
		return "", nil, fmt.Errorf("failed to write temp kubeconfig: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", nil, fmt.Errorf("failed to close temp kubeconfig: %w", err)
	}

	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	return tmpPath, cleanup, nil
}
