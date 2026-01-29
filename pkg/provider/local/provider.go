package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Provider implements the local K3s provider
type Provider struct{}

// NewProvider creates a new local provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "local"
}

// ConfigKey returns the YAML key for this provider's configuration
func (p *Provider) ConfigKey() string {
	return "local"
}

// Validate validates the local configuration
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating local provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", cfg.ProjectName))

	// Parse local provider config
	var localCfg Config
	if rawCfg := cfg.ProviderConfig["local"]; rawCfg != nil {
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &localCfg); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to unmarshal local config: %w", err)
		}
	}

	// Get the context name from config, default to "default" if not specified
	contextName := localCfg.KubeContext
	if contextName == "" {
		contextName = "default"
	}

	span.SetAttributes(attribute.String("kube_context", contextName))

	// Get kubeconfig file path
	kubeconfigPath := getKubeconfigPath()
	span.SetAttributes(attribute.String("kubeconfig_path", kubeconfigPath))

	// Verify kubeconfig file exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Kubeconfig file not found at %s", kubeconfigPath)).
			WithResource("provider").
			WithAction("validate").
			WithMetadata("kubeconfig_path", kubeconfigPath))
		return fmt.Errorf("kubeconfig file not found at %s", kubeconfigPath)
	}

	// Load and validate kubeconfig
	kubeconfigData, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to load kubeconfig from %s", kubeconfigPath)).
			WithResource("provider").
			WithAction("validate").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
	}

	// Verify the specified context exists
	if _, exists := kubeconfigData.Contexts[contextName]; !exists {
		availableContexts := getContextNames(kubeconfigData)
		err := fmt.Errorf("context %s not found in kubeconfig. Available contexts: %v", contextName, availableContexts)
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Context %s not found in kubeconfig", contextName)).
			WithResource("provider").
			WithAction("validate").
			WithMetadata("available_contexts", availableContexts))
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Successfully validated local provider configuration with context: %s", contextName)).
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("kube_context", contextName))

	return nil
}

// Deploy deploys local K3s infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if rawCfg := cfg.ProviderConfig["local"]; rawCfg != nil {
		var localCfg Config
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &localCfg); err == nil {
			span.SetAttributes(attribute.String("local.kube_context", localCfg.KubeContext))
		}
	}

	// Marshal config to JSON for status message
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Local provider deployment (stub)").
		WithResource("provider").
		WithAction("deploy").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("config", string(configJSON)))

	return nil
}

// Reconcile reconciles local infrastructure state (stub implementation)
func (p *Provider) Reconcile(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Reconcile")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Reconciling local provider (stub)").
		WithResource("provider").
		WithAction("reconcile").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// Destroy tears down local infrastructure (stub implementation)
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying local provider infrastructure (stub)").
		WithResource("provider").
		WithAction("destroy").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// GetKubeconfig retrieves kubeconfig for the specified context
func (p *Provider) GetKubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("cluster_name", cfg.ProjectName),
	)

	// Parse local provider config to get the kube context
	var localCfg Config
	if rawCfg := cfg.ProviderConfig["local"]; rawCfg != nil {
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &localCfg); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to unmarshal local config: %w", err)
		}
	}

	// Get the context name from config, default to "default" if not specified
	contextName := localCfg.KubeContext
	if contextName == "" {
		contextName = "default"
	}

	span.SetAttributes(attribute.String("kube_context", contextName))

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Retrieving kubeconfig for context: %s", contextName)).
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("kube_context", contextName))

	// Get kubeconfig file path
	kubeconfigPath := getKubeconfigPath()
	span.SetAttributes(attribute.String("kubeconfig_path", kubeconfigPath))

	// Load the kubeconfig file
	kubeconfigData, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to load kubeconfig from %s", kubeconfigPath)).
			WithResource("provider").
			WithAction("get-kubeconfig").
			WithMetadata("error", err.Error()))
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
	}

	// Verify the context exists
	if _, exists := kubeconfigData.Contexts[contextName]; !exists {
		err := fmt.Errorf("context %s not found in kubeconfig", contextName)
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Context %s not found in kubeconfig", contextName)).
			WithResource("provider").
			WithAction("get-kubeconfig").
			WithMetadata("available_contexts", getContextNames(kubeconfigData)))
		return nil, err
	}

	// Create a new kubeconfig with only the specified context
	filteredConfig := filterKubeconfigByContext(kubeconfigData, contextName)

	// Write the filtered kubeconfig to bytes
	kubeconfigBytes, err := clientcmd.Write(*filteredConfig)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	span.SetAttributes(attribute.Int("kubeconfig_size_bytes", len(kubeconfigBytes)))

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Successfully retrieved kubeconfig for context: %s", contextName)).
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("kube_context", contextName))

	return kubeconfigBytes, nil
}

// getKubeconfigPath returns the path to the kubeconfig file
// It checks KUBECONFIG env var first, then falls back to default location
func getKubeconfigPath() string {
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		return kubeconfigEnv
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".kube", "config")
	}
	return filepath.Join(homeDir, ".kube", "config")
}

// getContextNames returns a list of all context names in the kubeconfig
func getContextNames(config *clientcmdapi.Config) []string {
	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}
	return names
}

// filterKubeconfigByContext creates a new kubeconfig containing only the specified context
func filterKubeconfigByContext(config *clientcmdapi.Config, contextName string) *clientcmdapi.Config {
	context := config.Contexts[contextName]

	filtered := clientcmdapi.NewConfig()
	filtered.CurrentContext = contextName

	// Add the context
	filtered.Contexts[contextName] = context

	// Add the cluster referenced by the context
	if cluster, exists := config.Clusters[context.Cluster]; exists {
		filtered.Clusters[context.Cluster] = cluster
	}

	// Add the user referenced by the context
	if user, exists := config.AuthInfos[context.AuthInfo]; exists {
		filtered.AuthInfos[context.AuthInfo] = user
	}

	return filtered
}

// Summary returns key configuration details for display purposes
func (p *Provider) Summary(cfg *config.NebariConfig) map[string]string {
	result := make(map[string]string)

	rawCfg := cfg.ProviderConfig["local"]
	if rawCfg == nil {
		return result
	}

	var localCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &localCfg); err != nil {
		return result
	}

	if localCfg.KubeContext != "" {
		result["Kube Context"] = localCfg.KubeContext
	}
	return result
}
