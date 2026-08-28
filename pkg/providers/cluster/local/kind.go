package local

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	clusterapi "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

const (
	// kindReadyTimeout bounds how long cluster creation waits for the node
	// to become Ready. ArgoCD is installed immediately after Deploy, so we
	// need a schedulable node, not just a responding API server. This is fixed
	// on purpose and not wired through DeployOptions.Timeout which is meant to be
	// used for the whole deploy.
	kindReadyTimeout = 90 * time.Second

	// Default host ports publishing the gateway's listeners, used when
	// http_port / https_port are unset.
	defaultHTTPPort  int32 = 80
	defaultHTTPSPort int32 = 443
)

// kindContextName returns the kubeconfig context kind generates for a cluster.
func kindContextName(clusterName string) string {
	return "kind-" + clusterName
}

// newKindProvider builds a kind cluster provider backed by the detected container runtime
func newKindProvider() (*cluster.Provider, error) {
	opt, err := cluster.DetectNodeProvider()
	if err != nil {
		return nil, fmt.Errorf("detect container runtime for kind: %w", err)
	}
	return cluster.NewProvider(opt), nil
}

// kindClusterExists reports whether a kind cluster with the given name exists.
func kindClusterExists(ctx context.Context, kp *cluster.Provider, name string) (bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.kindClusterExists")
	defer span.End()
	span.SetAttributes(attribute.String("cluster_name", name))

	clusters, err := kp.List()
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("list kind clusters: %w", err)
	}
	return slices.Contains(clusters, name), nil
}

// hostPort narrows a config port to int32 for kind's PortMapping. Zero (the
// unset value) becomes def, and out-of-range values also fall back to def as
// a safety net: Validate has already rejected them by the time Deploy runs.
func hostPort(port int, def int32) int32 {
	if port <= 0 || port > 65535 {
		return def
	}
	return int32(port)
}

// gatewayPortMappings publishes the gateway's fixed NodePorts on host ports,
// so the platform is reachable at 127.0.0.1 without a routable load-balancer
// IP (which Docker Desktop on macOS/Windows cannot provide). The listen
// address is loopback on purpose: a local development cluster should not be
// exposed to the LAN.
func gatewayPortMappings(httpPort, httpsPort int) []v1alpha4.PortMapping {
	return []v1alpha4.PortMapping{
		{
			ContainerPort: clusterapi.GatewayHTTPNodePort,
			HostPort:      hostPort(httpPort, defaultHTTPPort),
			ListenAddress: "127.0.0.1",
			Protocol:      v1alpha4.PortMappingProtocolTCP,
		},
		{
			ContainerPort: clusterapi.GatewayHTTPSNodePort,
			HostPort:      hostPort(httpsPort, defaultHTTPSPort),
			ListenAddress: "127.0.0.1",
			Protocol:      v1alpha4.PortMappingProtocolTCP,
		},
	}
}

// createKindCluster creates a kind cluster with the configured node image and mounts.
// httpPort and httpsPort are the host ports publishing the gateway's listeners
// (0 means the defaults, 80 and 443).
func createKindCluster(ctx context.Context, kp *cluster.Provider, name string, kindCfg *KindConfig, httpPort, httpsPort int) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.createKindCluster")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", name),
		attribute.String("node_image", kindCfg.NodeImage),
		attribute.Int("extra_mounts", len(kindCfg.ExtraMounts)),
	)

	mounts := make([]v1alpha4.Mount, 0, 1+len(kindCfg.ExtraMounts))

	// Mount NIC's managed local gitops repo into the node. ArgoCD's
	// repo-server runs inside the cluster, so for it to read a file:// repo the
	// host directory has to be visible from within the node. kind requires a mount's
	// host path to exist when the cluster is created, so it gets created here if it
	// does not exist already
	defaultGitOps := config.DefaultLocalRepositoryPath(name)
	if err := git.EnsureLocalGitOpsDir(ctx, defaultGitOps); err != nil {
		span.RecordError(err)
		return err
	}
	mounts = append(mounts, v1alpha4.Mount{
		HostPath:      defaultGitOps,
		ContainerPath: defaultGitOps,
		Readonly:      true,
	})

	for _, m := range kindCfg.ExtraMounts {
		// Create custom mount roots with the historical restricted default.
		// Existing paths are untouched. GitOps bootstrap separately upgrades only
		// the root and Git-serving metadata of a configured file:// repository.
		if err := os.MkdirAll(m.HostPath, 0o750); err != nil {
			span.RecordError(err)
			return fmt.Errorf("create extra_mount host path %s: %w", m.HostPath, err)
		}
		mounts = append(mounts, v1alpha4.Mount{
			HostPath:      m.HostPath,
			ContainerPath: m.ContainerPath,
			Readonly:      m.ReadOnly,
		})
	}

	clusterConfig := &v1alpha4.Cluster{
		Name: name,
		Nodes: []v1alpha4.Node{
			{
				Role:              v1alpha4.ControlPlaneRole,
				ExtraMounts:       mounts,
				ExtraPortMappings: gatewayPortMappings(httpPort, httpsPort),
			},
		},
	}

	opts := []cluster.CreateOption{
		cluster.CreateWithV1Alpha4Config(clusterConfig),
		cluster.CreateWithWaitForReady(kindReadyTimeout),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	}
	if kindCfg.NodeImage != "" {
		opts = append(opts, cluster.CreateWithNodeImage(kindCfg.NodeImage))
	}

	if err := kp.Create(name, opts...); err != nil {
		span.RecordError(err)
		return fmt.Errorf("create kind cluster %s: %w", name, err)
	}
	return nil
}
