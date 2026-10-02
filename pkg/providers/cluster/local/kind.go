package local

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	clusterapi "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// kindReadyTimeout bounds how long cluster creation waits for the nodes
	// to become Ready. ArgoCD is installed immediately after Deploy, so we
	// need a schedulable node, not just a responding API server. kind itself
	// waits only for the control plane, so on a cluster with workers Deploy
	// waits for them separately (waitForNodesReady). This is fixed
	// on purpose and not wired through DeployOptions.Timeout which is meant to be
	// used for the whole deploy.
	kindReadyTimeout = 90 * time.Second

	// Default host ports publishing the gateway's listeners, used when
	// http_port / https_port are unset.
	defaultHTTPPort  int32 = 80
	defaultHTTPSPort int32 = 443

	// gatewayHostAddress is the host address the gateway's listeners are
	// published on, both as kind's listen address and as the address reported
	// through InfraSettings.GatewayHostAddress. Loopback on purpose: a local
	// development cluster should not be exposed to the LAN.
	gatewayHostAddress = "127.0.0.1"

	// controlPlaneLabel is the label kubeadm puts on control-plane nodes.
	// Nodes without it are the cluster's workers.
	controlPlaneLabel = "node-role.kubernetes.io/control-plane"
)

// nodeReadyPollInterval is how often waitForNodesReady checks the nodes of a
// multi-node cluster. A variable so tests can shorten it.
var nodeReadyPollInterval = 2 * time.Second

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
// a safety net.
func hostPort(port int, def int32) int32 {
	if port <= 0 || port > 65535 {
		return def
	}
	return int32(port)
}

// gatewayPortMappings publishes the gateway's fixed NodePorts on host ports
// of gatewayHostAddress, so the platform is reachable without a routable
// load-balancer IP (which Docker Desktop on macOS/Windows cannot provide).
func gatewayPortMappings(httpPort, httpsPort int) []v1alpha4.PortMapping {
	return []v1alpha4.PortMapping{
		{
			ContainerPort: clusterapi.GatewayHTTPNodePort,
			HostPort:      hostPort(httpPort, defaultHTTPPort),
			ListenAddress: gatewayHostAddress,
			Protocol:      v1alpha4.PortMappingProtocolTCP,
		},
		{
			ContainerPort: clusterapi.GatewayHTTPSNodePort,
			HostPort:      hostPort(httpsPort, defaultHTTPSPort),
			ListenAddress: gatewayHostAddress,
			Protocol:      v1alpha4.PortMappingProtocolTCP,
		},
	}
}

// createKindCluster creates a kind cluster with the configured node image and mounts.
// httpPort and httpsPort are the host ports publishing the gateway's listeners
// (0 means the defaults, 80 and 443).
func createKindCluster(ctx context.Context, kp *cluster.Provider, name string, kindCfg *KindConfig, httpPort, httpsPort int) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.createKindCluster")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", name),
		attribute.String("node_image", kindCfg.NodeImage),
		attribute.Int("extra_mounts", len(kindCfg.ExtraMounts)),
		attribute.Int("worker_nodes", kindCfg.WorkerCount()),
	)

	mounts := make([]v1alpha4.Mount, 0, 1+len(kindCfg.ExtraMounts))

	// Mount NIC's managed local gitops repo into the nodes. ArgoCD's
	// repo-server runs inside the cluster, so for it to read a file:// repo the
	// host directory has to be visible from within whichever node it runs on
	// (kindNodes gives every node these mounts). kind requires a mount's
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

	// Create custom mount roots with the historical restricted default.
	// Existing paths are untouched. GitOps bootstrap separately upgrades only
	// the root and Git-serving metadata of a configured file:// repository.
	// kind requires every host path to exist, the node groups' too.
	hostPaths := make([]string, 0, len(kindCfg.ExtraMounts))
	for _, m := range kindCfg.ExtraMounts {
		hostPaths = append(hostPaths, m.HostPath)
	}
	for _, g := range kindCfg.NodeGroups {
		for _, m := range g.ExtraMounts {
			hostPaths = append(hostPaths, m.HostPath)
		}
	}
	for _, hp := range hostPaths {
		if err := os.MkdirAll(hp, 0o750); err != nil {
			span.RecordError(err)
			return fmt.Errorf("create extra_mount host path %s: %w", hp, err)
		}
	}
	mounts = append(mounts, kindMounts(kindCfg.ExtraMounts)...)

	clusterConfig := &v1alpha4.Cluster{
		Name:  name,
		Nodes: kindNodes(ctx, kindCfg, mounts, httpPort, httpsPort),
	}

	opts := []cluster.CreateOption{
		cluster.CreateWithV1Alpha4Config(clusterConfig),
		cluster.CreateWithWaitForReady(kindReadyTimeout),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	}
	// Images are set per node by kindNodes. cluster.CreateWithNodeImage is
	// not used: it overrides every node's image, node groups' included.

	if err := kp.Create(name, opts...); err != nil {
		span.RecordError(err)
		return fmt.Errorf("create kind cluster %s: %w", name, err)
	}
	return nil
}

// kindMounts converts config mounts to kind's mount type.
func kindMounts(mounts []KindMount) []v1alpha4.Mount {
	out := make([]v1alpha4.Mount, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, v1alpha4.Mount{
			HostPath:      m.HostPath,
			ContainerPath: m.ContainerPath,
			Readonly:      m.ReadOnly,
		})
	}
	return out
}

// kindNodes builds the cluster's node list: one control plane, then each node
// group's Count workers, groups in name order. shared are the mounts every
// node gets, so a pod reading one (ArgoCD's repo-server for a file:// GitOps
// repo) can schedule on any node; a group's own mounts and labels go on its
// nodes only. Only the control plane gets the gateway's port mappings, since
// a host port can be bound once; the pinned NodePorts forward to Envoy
// wherever it runs (see externalTrafficPolicy in the EnvoyProxy manifest).
// With workers present, kind keeps the control plane tainted, so workloads
// run on the workers. An empty image leaves kind to apply its default.
func kindNodes(ctx context.Context, kindCfg *KindConfig, shared []v1alpha4.Mount, httpPort, httpsPort int) []v1alpha4.Node {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.kindNodes")
	defer span.End()
	span.SetAttributes(
		attribute.Int("worker_nodes", kindCfg.WorkerCount()),
		attribute.Int("shared_mounts", len(shared)),
	)

	nodes := []v1alpha4.Node{
		{
			Role:              v1alpha4.ControlPlaneRole,
			Image:             kindCfg.NodeImage,
			ExtraMounts:       slices.Clone(shared),
			ExtraPortMappings: gatewayPortMappings(httpPort, httpsPort),
		},
	}
	for _, name := range kindCfg.groupNames() {
		g := kindCfg.NodeGroups[name]
		image := g.Image
		if image == "" {
			image = kindCfg.NodeImage
		}
		groupMounts := kindMounts(g.ExtraMounts)
		for range g.Count {
			labels := maps.Clone(g.Labels)
			if labels == nil {
				labels = map[string]string{}
			}
			labels[nodeGroupLabel] = name
			nodes = append(nodes, v1alpha4.Node{
				Role:        v1alpha4.WorkerRole,
				Image:       image,
				Labels:      labels,
				ExtraMounts: slices.Concat(shared, groupMounts),
			})
		}
	}
	return nodes
}

// waitForNodesReady waits until want nodes are registered and Ready. kind's
// own ready-wait (kindReadyTimeout) covers only the control plane, which
// stays tainted once workers exist, so on a multi-node cluster it can return
// before any node can take workloads.
func waitForNodesReady(ctx context.Context, client kubernetes.Interface, clusterName string, want int, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.waitForNodesReady")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Int("want_nodes", want),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Waiting for all %d nodes of kind cluster %s to be Ready", want, clusterName)).
		WithResource("provider").
		WithAction("deploy").
		WithMetadata("cluster_name", clusterName))

	var (
		ready    int
		notReady []string
		// lastErr is the most recent node list error, cleared by a list that
		// succeeds. PollUntilContextTimeout only returns the context's own
		// error, so without it an unreachable or forbidden API would read
		// as nodes that never got Ready.
		lastErr error
	)
	err := wait.PollUntilContextTimeout(ctx, nodeReadyPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			// Transient while nodes join; keep polling until the timeout.
			lastErr = err
			return false, nil
		}
		lastErr = nil
		ready = 0
		notReady = notReady[:0]
		for _, n := range nodes.Items {
			if nodeIsReady(n) {
				ready++
			} else {
				notReady = append(notReady, n.Name)
			}
		}
		return ready >= want, nil
	})
	span.SetAttributes(attribute.Int("ready_nodes", ready))
	if err != nil {
		err = fmt.Errorf("kind cluster %s: only %d of %d nodes Ready after %s: %w", clusterName, ready, want, timeout, err)
		switch {
		case lastErr != nil:
			err = fmt.Errorf("%w (last node list error: %v)", err, lastErr)
		case len(notReady) > 0:
			slices.Sort(notReady)
			err = fmt.Errorf("%w (not Ready: %s)", err, strings.Join(notReady, ", "))
		default:
			err = fmt.Errorf("%w (only %d nodes registered)", err, ready)
		}
		span.RecordError(err)
		return err
	}
	return nil
}

// nodeIsReady reports whether the node's Ready condition is True.
func nodeIsReady(n corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// waitForClusterNodes makes Deploy's post-create or post-reuse wait. kind
// waited for the control plane only, and with workers it stays tainted, so
// the workers are waited for before anything is scheduled. A new cluster is
// waited on for its configured workers. A reused one is waited on for the
// workers it actually has (a retry after the create-time wait timed out may
// still have some joining): a count mismatch only warns, so it must not
// become a timeout here, and a failed node list skips the wait for the same
// reason. A single-node cluster is not waited on at all.
func waitForClusterNodes(ctx context.Context, client kubernetes.Interface, clusterName string, configuredWorkers int, reused bool, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.waitForClusterNodes")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Int("configured_workers", configuredWorkers),
		attribute.Bool("reused", reused),
	)

	workers := configuredWorkers
	if reused {
		workers = checkClusterWorkers(ctx, client, clusterName, configuredWorkers)
	}
	span.SetAttributes(attribute.Int("wait_workers", workers))
	if workers <= 0 {
		return nil
	}
	if err := waitForNodesReady(ctx, client, clusterName, 1+workers, timeout); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// checkClusterWorkers compares the configured worker count against the
// worker nodes of an existing cluster and warns when they differ. kind sets
// the node list at creation only, so a changed count needs a recreate. A
// mismatch leaves a working cluster of a different size, unlike a changed
// host port, so it warns rather than failing the deploy. For the same reason
// a failure to list the nodes is a warning too. It returns the cluster's
// actual worker count, or -1 when the nodes could not be listed.
func checkClusterWorkers(ctx context.Context, client kubernetes.Interface, clusterName string, configured int) int {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.checkClusterWorkers")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Int("configured_workers", configured),
	)

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Kind cluster %s: could not check the worker node count against the config: %v", clusterName, err)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", clusterName))
		return -1
	}
	actual := 0
	for _, n := range nodes.Items {
		if _, ok := n.Labels[controlPlaneLabel]; !ok {
			actual++
		}
	}
	span.SetAttributes(attribute.Int("actual_workers", actual))

	if actual != configured {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Kind cluster %s has %d worker node(s) but the config's node_groups add up to %d. kind sets the nodes at cluster creation only, so recreate the cluster (nic destroy, then nic deploy --regen-apps, so a GitOps repository bootstrapped by an earlier NIC version picks up the multi-node gateway settings) to apply the change", clusterName, actual, configured)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", clusterName))
	}
	return actual
}
