package local

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"slices"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

const (
	// kindReadyTimeout bounds how long cluster creation waits for the node
	// to become Ready. ArgoCD is installed immediately after Deploy, so we
	// need a schedulable node, not just a responding API server. This is fixed
	// on purpose and not wired through DeployOptions.Timeout which is meant to be
	// used for the whole deploy.
	kindReadyTimeout = 90 * time.Second
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

// createKindCluster creates a kind cluster with the configured node image and mounts
func createKindCluster(ctx context.Context, kp *cluster.Provider, name string, kindCfg *KindConfig) error {
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
	defaultGitOps := config.DefaultLocalRepoPath(name)
	if err := os.MkdirAll(defaultGitOps, 0o750); err != nil {
		span.RecordError(err)
		return fmt.Errorf("create local gitops directory %s: %w", defaultGitOps, err)
	}
	mounts = append(mounts, v1alpha4.Mount{
		HostPath:      defaultGitOps,
		ContainerPath: defaultGitOps,
		Readonly:      true,
	})

	for _, m := range kindCfg.ExtraMounts {
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
				Role:        v1alpha4.ControlPlaneRole,
				ExtraMounts: mounts,
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

// kindNodeAddressPool derives a MetalLB address pool from a cluster node's
// IPv4 address, so LoadBalancer IPs are routable on whatever subnet the kind
// Docker network actually has rather than a hardcoded default. It reads the
// address through kind's node abstraction (nodes.Node.IP), so it works for
// docker and podman alike without NIC shelling out to a container runtime.
func kindNodeAddressPool(ctx context.Context, kp *cluster.Provider, name string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.kindNodeAddressPool")
	defer span.End()
	span.SetAttributes(attribute.String("cluster_name", name))

	nodeList, err := kp.ListNodes(name)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("list nodes for kind cluster %s: %w", name, err)
	}
	if len(nodeList) == 0 {
		err := fmt.Errorf("kind cluster %s has no nodes", name)
		span.RecordError(err)
		return "", err
	}
	// Any node works because all kind clusters share the "kind" Docker network,
	// so every node sits on the same subnet.
	ipv4, _, err := nodeList[0].IP()
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("get node IP for kind cluster %s: %w", name, err)
	}
	if ipv4 == "" {
		err := fmt.Errorf("kind cluster %s node has no IPv4 address", name)
		span.RecordError(err)
		return "", err
	}
	// kind creates its Docker network from Docker's default address pools,
	// which are /16. ParseCIDR (in deriveAddressPool) masks the host bits, so
	// "<nodeIP>/16" yields the network the pool should live in.
	return deriveAddressPool(ipv4 + "/16")
}

// deriveAddressPool maps an IPv4 subnet to an 11-address MetalLB pool in the
// .100-.110 range of the subnet's last /24 block, e.g. 172.18.0.0/16 ->
// 172.18.255.100-172.18.255.110 and 192.168.1.0/24 -> 192.168.1.100-192.168.1.110.
func deriveAddressPool(subnet string) (string, error) {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("parse subnet %q: %w", subnet, err)
	}
	ip4 := ipnet.IP.To4()
	if ip4 == nil {
		return "", fmt.Errorf("subnet %q is not IPv4", subnet)
	}
	ones, _ := ipnet.Mask.Size()
	if ones > 24 {
		return "", fmt.Errorf("subnet %q is smaller than /24", subnet)
	}

	base := binary.BigEndian.Uint32(ip4)
	size := uint32(1) << (32 - ones)
	lastBlock := base + size - 256

	toIP := func(v uint32) net.IP {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, v)
		return ip
	}
	return fmt.Sprintf("%s-%s", toIP(lastBlock+100), toIP(lastBlock+110)), nil
}
