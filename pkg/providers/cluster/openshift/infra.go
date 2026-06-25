package openshift

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// InfraSettings returns OpenShift-specific Kubernetes infrastructure settings.
// Implemented in Task B3.
func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) cluster.InfraSettings {
	return cluster.InfraSettings{StorageClass: defaultStorageClass}
}
