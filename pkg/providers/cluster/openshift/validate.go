package openshift

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Validate checks the OpenShift configuration before any infrastructure
// operations. Implemented in Task B4.
func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	_ = projectName
	_, err := extractConfig(ctx, clusterConfig)
	return err
}
