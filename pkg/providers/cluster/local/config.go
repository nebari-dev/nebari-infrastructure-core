package local

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/util/validation"
)

// nodeGroupLabel is the label NIC puts on every worker node with the name of
// its node group.
const nodeGroupLabel = "nebari.dev/node-group"

// kubeletLabelKeys and kubeletLabelNamespaces are the kubernetes.io labels a
// kubelet may set on its own node. Any other key in the kubernetes.io or
// k8s.io namespaces makes the kubelet refuse to start. Mirrors
// k8s.io/kubelet/pkg/apis.IsKubeletLabel and ValidateKubeletFlags in
// cmd/kubelet/app/options, which are not importable libraries.
var (
	kubeletLabelKeys = []string{
		"kubernetes.io/hostname",
		"topology.kubernetes.io/zone",
		"topology.kubernetes.io/region",
		"failure-domain.beta.kubernetes.io/zone",
		"failure-domain.beta.kubernetes.io/region",
		"beta.kubernetes.io/instance-type",
		"node.kubernetes.io/instance-type",
		"kubernetes.io/os",
		"kubernetes.io/arch",
		"beta.kubernetes.io/os",
		"beta.kubernetes.io/arch",
	}
	kubeletLabelNamespaces = []string{"kubelet.kubernetes.io", "node.kubernetes.io"}
)

// Config represents local provider configuration
type Config struct {
	Kind          *KindConfig                  `yaml:"kind,omitempty"`
	NodeSelectors map[string]map[string]string `yaml:"node_selectors,omitempty"`
	// HTTPSPort is the host port the gateway's HTTPS listener is published on
	// (default 443). Override it when 443 is taken on the host or when running
	// several local clusters side by side. Takes effect on cluster creation
	// only. kind port mappings cannot be changed on an existing cluster.
	HTTPSPort int `yaml:"https_port,omitempty"`
	// HTTPPort is the host port the gateway's HTTP listener (the HTTPS
	// redirect) is published on (default 80). Override it under the same
	// circumstances as HTTPSPort, including rootless container runtimes that
	// cannot bind ports below 1024. Takes effect on cluster creation only.
	HTTPPort         int            `yaml:"http_port,omitempty"`
	AdditionalFields map[string]any `yaml:",inline"`
}

// KindConfig holds optional config for the deployed kind cluster. It may be
// omitted entirely (nil), in which case the cluster is created with defaults.
type KindConfig struct {
	// NodeImage is the kindest/node image to use (e.g. "kindest/node:v1.32.2").
	// Empty means the default image of the bundled kind version.
	NodeImage string `yaml:"node_image,omitempty"`

	// ExtraMounts are additional host directories mounted into every cluster node
	// container. NIC mounts its auto-created local GitOps repository
	// automatically; an explicit file:// repository needs a matching entry here.
	// Other custom mounts are user-managed, and NIC does not recursively
	// normalize their permissions.
	ExtraMounts []KindMount `yaml:"extra_mounts,omitempty"`

	// NodeGroups are the cluster's worker nodes, keyed by group name. NIC
	// always creates exactly one control-plane node, which is not
	// configurable here: node_groups defines workers only. With no node
	// groups, the control plane is the only node and runs everything. With
	// any, kind keeps the control plane tainted, so workloads run on the
	// groups' nodes and only system pods stay on the control plane. The
	// control plane uses node_image and the shared extra_mounts, publishes
	// the gateway host ports, and never takes a group's labels or mounts.
	// Takes effect on cluster creation only.
	NodeGroups map[string]KindNodeGroup `yaml:"node_groups,omitempty"`
}

// KindNodeGroup is a set of identical kind worker nodes. Every node in the
// group is labeled nebari.dev/node-group=<group name>, so workloads can
// target a group with a nodeSelector without extra labels.
type KindNodeGroup struct {
	// Count is the number of worker nodes in the group (at least 1).
	Count int `yaml:"count"`

	// Image overrides node_image for this group's nodes (e.g.
	// "kindest/node:v1.32.2"). Empty means node_image, or kind's default
	// image when that is unset too. kind allows nodes of different
	// Kubernetes versions, within the usual version skew limits.
	Image string `yaml:"image,omitempty"`

	// Labels are added to every node in the group. Keys in the kubernetes.io
	// and k8s.io namespaces are rejected unless the kubelet may set them
	// itself (node.kubernetes.io/ and kubelet.kubernetes.io/ prefixes, for
	// example), since the kubelet refuses to start with any other.
	Labels map[string]string `yaml:"labels,omitempty"`

	// ExtraMounts are mounted into this group's nodes only, in addition to
	// the shared extra_mounts and NIC's GitOps mount.
	ExtraMounts []KindMount `yaml:"extra_mounts,omitempty"`
}

// KindMount mounts a host directory into every kind node container.
type KindMount struct {
	HostPath      string `yaml:"host_path"`
	ContainerPath string `yaml:"container_path"`
	ReadOnly      bool   `yaml:"read_only,omitempty"`
}

// WorkerCount returns the total number of worker nodes across all node
// groups. A nil KindConfig has none.
func (k *KindConfig) WorkerCount() int {
	if k == nil {
		return 0
	}
	total := 0
	for _, g := range k.NodeGroups {
		total += g.Count
	}
	return total
}

// groupNames returns the node group names in sorted order. kind names worker
// containers by position, so expanding the groups in map order would move
// labels and mounts between containers from one run to the next.
func (k *KindConfig) groupNames() []string {
	if k == nil {
		return nil
	}
	names := make([]string, 0, len(k.NodeGroups))
	for name := range k.NodeGroups {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Validate checks the local provider config. Both Provider.Validate and
// Provider.Deploy call it, since Validate is not on the deploy path.
func (c Config) Validate(ctx context.Context) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Config.Validate")
	defer span.End()
	span.SetAttributes(
		attribute.Int("node_groups", len(c.Kind.groupNames())),
		attribute.Int("worker_nodes", c.Kind.WorkerCount()),
	)

	err := c.validate()
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func (c Config) validate() error {
	if c.Kind != nil {
		if err := validateMounts("kind extra_mounts", c.Kind.ExtraMounts, nil); err != nil {
			return err
		}
		for _, name := range c.Kind.groupNames() {
			if err := validateNodeGroup(name, c.Kind.NodeGroups[name], c.Kind.ExtraMounts); err != nil {
				return err
			}
		}
	}

	for _, p := range []struct {
		name string
		port int
	}{
		{"http_port", c.HTTPPort},
		{"https_port", c.HTTPSPort},
	} {
		if p.port < 0 || p.port > 65535 {
			return fmt.Errorf("%s must be between 1 and 65535 or omitted, got %d", p.name, p.port)
		}
	}

	// Compare the ports kind will actually map (hostPort applies the 80/443
	// defaults), so a collision with a defaulted port is also caught. kind
	// rejects duplicate port mappings too, but only after provisioning has
	// started.
	if hostPort(c.HTTPPort, defaultHTTPPort) == hostPort(c.HTTPSPort, defaultHTTPSPort) {
		return fmt.Errorf("http_port and https_port must differ, both are %d", hostPort(c.HTTPPort, defaultHTTPPort))
	}
	return nil
}

// validateNodeGroup checks one node group. shared are the extra_mounts every
// node gets, which the group's own mounts must not collide with.
func validateNodeGroup(name string, g KindNodeGroup, shared []KindMount) error {
	field := fmt.Sprintf("kind node_groups[%q]", name)
	// The name becomes a label value and part of status messages.
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return fmt.Errorf("%s: name must be a lowercase DNS label: %s", field, strings.Join(errs, "; "))
	}
	if g.Count < 1 {
		return fmt.Errorf("%s.count must be at least 1, got %d", field, g.Count)
	}

	keys := make([]string, 0, len(g.Labels))
	for k := range g.Labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if errs := validation.IsQualifiedName(k); len(errs) > 0 {
			return fmt.Errorf("%s.labels: invalid key %q: %s", field, k, strings.Join(errs, "; "))
		}
		if errs := validation.IsValidLabelValue(g.Labels[k]); len(errs) > 0 {
			return fmt.Errorf("%s.labels: invalid value %q for %q: %s", field, g.Labels[k], k, strings.Join(errs, "; "))
		}
		if k == nodeGroupLabel {
			return fmt.Errorf("%s.labels: %q is set by NIC to the group name", field, k)
		}
		if isReservedNodeLabel(k) {
			return fmt.Errorf("%s.labels: %q is reserved (the kubelet only accepts kubernetes.io and k8s.io labels under %s, or one of %s)",
				field, k, strings.Join(kubeletLabelNamespaces, ", "), strings.Join(kubeletLabelKeys, ", "))
		}
	}

	return validateMounts(field+".extra_mounts", g.ExtraMounts, shared)
}

// validateMounts checks that mounts use absolute paths and that no container
// path is mounted twice on a node, counting the shared mounts the node also
// gets.
func validateMounts(field string, mounts, shared []KindMount) error {
	seen := make(map[string]bool, len(shared)+len(mounts))
	for _, m := range shared {
		seen[filepath.Clean(m.ContainerPath)] = true
	}
	for _, m := range mounts {
		if !filepath.IsAbs(m.HostPath) || !filepath.IsAbs(m.ContainerPath) {
			return fmt.Errorf("%s paths must be absolute: %s -> %s", field, m.HostPath, m.ContainerPath)
		}
		cp := filepath.Clean(m.ContainerPath)
		if seen[cp] {
			return fmt.Errorf("%s: container_path %s is mounted more than once", field, cp)
		}
		seen[cp] = true
	}
	return nil
}

// isReservedNodeLabel reports whether the kubelet would refuse key as one of
// its --node-labels: a kubernetes.io or k8s.io label outside the set a
// kubelet may set on its own node.
func isReservedNodeLabel(key string) bool {
	ns, _, ok := strings.Cut(key, "/")
	if !ok {
		return false
	}
	inNamespace := func(ns, parent string) bool {
		return ns == parent || strings.HasSuffix(ns, "."+parent)
	}
	if !inNamespace(ns, "kubernetes.io") && !inNamespace(ns, "k8s.io") {
		return false
	}
	if slices.Contains(kubeletLabelKeys, key) {
		return false
	}
	for _, allowed := range kubeletLabelNamespaces {
		if inNamespace(ns, allowed) {
			return false
		}
	}
	return true
}
