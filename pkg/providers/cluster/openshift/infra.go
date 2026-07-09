package openshift

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// InfraSettings returns OpenShift-specific Kubernetes infrastructure settings
// used by the ArgoCD writer and deploy command to configure Nebari's
// foundational stack without importing this package.
//
// StorageClass defaults to the native ROSA CSI class (gp3-csi); operators can
// override it (e.g. "managed-csi" on ARO) via storage_class. NeedsMetalLB is
// false because ROSA provides AWS load balancers.
//
// LoadBalancerAnnotations request an AWS Network Load Balancer for the Gateway's
// Service. ROSA uses the in-tree AWS cloud provider / CCM by default (the AWS
// Load Balancer Controller is NOT installed), so the correct request is
// "aws-load-balancer-type: nlb" — NOT the AWS-LBC-only "external" value, which
// would leave the Service stuck pending on a stock ROSA cluster.
func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) cluster.InfraSettings {
	sc := defaultStorageClass

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg != nil {
		var cfg Config
		if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &cfg); err == nil {
			sc = cfg.StorageClassOrDefault()
		}
	}

	return cluster.InfraSettings{
		StorageClass:        sc,
		NeedsMetalLB:        false,
		SupportsLocalGitOps: false,
		LoadBalancerAnnotations: map[string]string{
			"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
		},
	}
}
