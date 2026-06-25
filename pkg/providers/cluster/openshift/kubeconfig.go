package openshift

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// GetKubeconfig returns a kubeconfig for the OpenShift cluster.
// Implemented in Task B7.
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	_ = ctx
	_ = projectName
	_ = clusterConfig
	return nil, fmt.Errorf("openshift: GetKubeconfig not yet implemented")
}
