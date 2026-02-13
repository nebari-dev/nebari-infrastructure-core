// Package kubeconfig provides helper functions for working with kubeconfig files.
package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// GetPath returns the path to the kubeconfig file.
// It checks the KUBECONFIG environment variable first, then falls back to ~/.kube/config.
func GetPath() string {
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		return kubeconfigEnv
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".kube", "config")
	}
	return filepath.Join(homeDir, ".kube", "config")
}

// Load loads the kubeconfig from the default path.
func Load() (*clientcmdapi.Config, error) {
	return LoadFromPath(GetPath())
}

// LoadFromPath loads the kubeconfig from the specified path.
func LoadFromPath(path string) (*clientcmdapi.Config, error) {
	return clientcmd.LoadFromFile(path)
}

// GetContextNames returns a list of all context names in the kubeconfig.
func GetContextNames(config *clientcmdapi.Config) []string {
	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}
	return names
}

// FilterByContext creates a new kubeconfig containing only the specified context
// and its associated cluster and user credentials.
func FilterByContext(config *clientcmdapi.Config, contextName string) (*clientcmdapi.Config, error) {
	context, exists := config.Contexts[contextName]
	if !exists {
		return nil, fmt.Errorf("context %q not found in kubeconfig", contextName)
	}

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

	return filtered, nil
}

// WriteBytes serializes the kubeconfig to bytes.
func WriteBytes(config *clientcmdapi.Config) ([]byte, error) {
	return clientcmd.Write(*config)
}

// ExtractContext loads the kubeconfig, filters it to the specified context,
// and returns the serialized bytes. This is a convenience function combining
// Load, FilterByContext, and WriteBytes.
func ExtractContext(contextName string) ([]byte, error) {
	config, err := Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	filtered, err := FilterByContext(config, contextName)
	if err != nil {
		return nil, err
	}

	return WriteBytes(filtered)
}

// ValidateContext checks that the specified context exists in the kubeconfig.
// Returns an error with available contexts if not found.
func ValidateContext(contextName string) error {
	config, err := Load()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig from %s: %w", GetPath(), err)
	}

	if _, exists := config.Contexts[contextName]; !exists {
		return fmt.Errorf("context %q not found in kubeconfig. Available contexts: %v", contextName, GetContextNames(config))
	}

	return nil
}
