package argocd

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
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

const (
	repoName  = "argo"
	repoURL   = "https://argoproj.github.io/argo-helm"
	chartName = "argo/argo-cd"
)

// loadArgoCDChart locates and loads the Argo CD Helm chart.
// This is extracted to avoid duplication between install and upgrade operations.
func loadArgoCDChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(chartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate Argo CD chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Argo CD chart: %w", err)
	}

	return loadedChart, nil
}

// InstallHelm installs Argo CD using the Helm Go SDK
func InstallHelm(ctx context.Context, kubeconfigBytes []byte, config Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.InstallHelm")
	defer span.End()

	span.SetAttributes(
		attribute.String("version", config.Version),
		attribute.String("namespace", config.Namespace),
		attribute.String("release_name", config.ReleaseName),
	)

	// Write kubeconfig to a temporary file for Helm to use
	tmpKubeconfig, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create temp kubeconfig: %w", err)
	}
	defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()

	if _, err := tmpKubeconfig.Write(kubeconfigBytes); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write temp kubeconfig: %w", err)
	}
	if err := tmpKubeconfig.Close(); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to close temp kubeconfig: %w", err)
	}

	// Create Helm action configuration
	actionConfig, err := newHelmActionConfig(tmpKubeconfig.Name(), config.Namespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	// Check if release already exists (idempotency)
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if releases, err := histClient.Run(config.ReleaseName); err == nil {
		// Release exists - check if upgrade is actually needed
		current := releases[0]
		if current.Chart != nil && current.Chart.Metadata != nil &&
			current.Chart.Metadata.Version == config.Version {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Argo CD already up to date, skipping").
				WithResource("argocd").
				WithAction("up-to-date").
				WithMetadata("version", config.Version))
			return nil
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Argo CD already installed, upgrading").
			WithResource("argocd").
			WithAction("upgrading"))
		return upgradeHelm(ctx, actionConfig, config)
	}

	// Add Argo CD Helm repository
	if err := addHelmRepo(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add Argo CD Helm repository: %w", err)
	}

	// Install Argo CD
	client := action.NewInstall(actionConfig)
	client.Namespace = config.Namespace
	client.ReleaseName = config.ReleaseName
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = config.Timeout

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Argo CD Helm chart").
		WithResource("argocd").
		WithAction("installing").
		WithMetadata("chart_version", config.Version))

	// Locate and load the chart
	chart, err := loadArgoCDChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Install the chart
	release, err := client.Run(chart, config.Values)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install Argo CD: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD Helm chart installed").
		WithResource("argocd").
		WithAction("installed").
		WithMetadata("chart_version", config.Version).
		WithMetadata("release_version", release.Version))

	return nil
}

// upgradeHelm upgrades an existing Argo CD installation
func upgradeHelm(ctx context.Context, actionConfig *action.Configuration, config Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.upgradeHelm")
	defer span.End()

	client := action.NewUpgrade(actionConfig)
	client.Namespace = config.Namespace
	client.Wait = true
	client.Timeout = config.Timeout

	// Locate and load the chart
	chart, err := loadArgoCDChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Upgrade the chart
	release, err := client.Run(config.ReleaseName, chart, config.Values)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade Argo CD: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD Helm chart upgraded").
		WithResource("argocd").
		WithAction("upgraded").
		WithMetadata("chart_version", config.Version).
		WithMetadata("release_version", release.Version))

	return nil
}

// newHelmActionConfig creates a new Helm action configuration
func newHelmActionConfig(kubeconfigPath string, namespace string) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	// Initialize with kubeconfig
	if err := actionConfig.Init(
		&kubeconfigGetter{path: kubeconfigPath},
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

// kubeconfigGetter implements the Helm RESTClientGetter interface
type kubeconfigGetter struct {
	path string
}

func (k *kubeconfigGetter) ToRESTConfig() (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags("", k.path)
}

func (k *kubeconfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
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

func (k *kubeconfigGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := k.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	return mapper, nil
}

func (k *kubeconfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: k.path},
		&clientcmd.ConfigOverrides{},
	)
}

// addHelmRepo adds the Argo CD Helm repository
func addHelmRepo(ctx context.Context) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.addHelmRepo")
	defer span.End()

	settings := cli.New()

	// Create repo entry
	entry := &repo.Entry{
		Name: repoName,
		URL:  repoURL,
	}

	// Get repo file
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

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Added Argo CD Helm repository").
		WithResource("helm-repo").
		WithAction("added").
		WithMetadata("repo_name", repoName).
		WithMetadata("repo_url", repoURL))

	return nil
}
