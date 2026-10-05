package local

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	clusterapi "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

func TestGatewayPortMappings(t *testing.T) {
	tests := []struct {
		name          string
		httpPort      int
		httpsPort     int
		wantHostHTTP  int32
		wantHostHTTPS int32
	}{
		{
			name:          "default ports",
			wantHostHTTP:  80,
			wantHostHTTPS: 443,
		},
		{
			name:          "custom ports for occupied 80/443 or rootless runtimes",
			httpPort:      8080,
			httpsPort:     8443,
			wantHostHTTP:  8080,
			wantHostHTTPS: 8443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings := gatewayPortMappings(tt.httpPort, tt.httpsPort)
			if len(mappings) != 2 {
				t.Fatalf("gatewayPortMappings(%d, %d) returned %d mappings, want 2", tt.httpPort, tt.httpsPort, len(mappings))
			}

			http, https := mappings[0], mappings[1]
			if http.ContainerPort != clusterapi.GatewayHTTPNodePort || http.HostPort != tt.wantHostHTTP {
				t.Errorf("http mapping = %d->%d, want %d->%d", http.HostPort, http.ContainerPort, tt.wantHostHTTP, clusterapi.GatewayHTTPNodePort)
			}
			if https.ContainerPort != clusterapi.GatewayHTTPSNodePort || https.HostPort != tt.wantHostHTTPS {
				t.Errorf("https mapping = %d->%d, want %d->%d", https.HostPort, https.ContainerPort, tt.wantHostHTTPS, clusterapi.GatewayHTTPSNodePort)
			}

			for _, m := range mappings {
				// Loopback on purpose: a development cluster must not be
				// published on the LAN.
				if m.ListenAddress != "127.0.0.1" {
					t.Errorf("mapping %d->%d listens on %q, want 127.0.0.1", m.HostPort, m.ContainerPort, m.ListenAddress)
				}
				if m.Protocol != v1alpha4.PortMappingProtocolTCP {
					t.Errorf("mapping %d->%d protocol = %q, want TCP", m.HostPort, m.ContainerPort, m.Protocol)
				}
			}
		})
	}
}

func TestKindContextName(t *testing.T) {
	if got := kindContextName("my-nebari-local"); got != "kind-my-nebari-local" {
		t.Errorf("kindContextName = %q, want %q", got, "kind-my-nebari-local")
	}
}

func TestKindNodes(t *testing.T) {
	shared := []v1alpha4.Mount{
		{HostPath: "/gitops", ContainerPath: "/gitops", Readonly: true},
		{HostPath: "/data", ContainerPath: "/data"},
	}
	models := v1alpha4.Mount{HostPath: "/host/models", ContainerPath: "/models", Readonly: true}

	// wantNode describes one expected node; the control plane is always first.
	type wantNode struct {
		role   v1alpha4.NodeRole
		image  string
		labels map[string]string
		mounts []v1alpha4.Mount
	}
	controlPlane := func(image string) wantNode {
		return wantNode{role: v1alpha4.ControlPlaneRole, image: image, mounts: shared}
	}

	tests := []struct {
		name    string
		kindCfg *KindConfig
		want    []wantNode
	}{
		{
			name:    "no node groups keeps the single-node cluster",
			kindCfg: &KindConfig{},
			want:    []wantNode{controlPlane("")},
		},
		{
			// Empty images leave kind to apply its default node image.
			name: "one group without images",
			kindCfg: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"general": {Count: 2},
			}},
			want: []wantNode{
				controlPlane(""),
				{role: v1alpha4.WorkerRole, labels: map[string]string{nodeGroupLabel: "general"}, mounts: shared},
				{role: v1alpha4.WorkerRole, labels: map[string]string{nodeGroupLabel: "general"}, mounts: shared},
			},
		},
		{
			// Groups expand in name order (kind names workers by position,
			// so map order would shuffle labels between runs). node_image is
			// every node's default and a group image overrides it for that
			// group only. Group labels and mounts never reach the control
			// plane or another group.
			name: "several groups expand in name order with their own settings",
			kindCfg: &KindConfig{
				NodeImage: "kindest/node:v1.33.0",
				NodeGroups: map[string]KindNodeGroup{
					"infra": {
						Count:       1,
						Image:       "kindest/node:v1.32.2",
						Labels:      map[string]string{"dedicated": "infra"},
						ExtraMounts: []KindMount{{HostPath: "/host/models", ContainerPath: "/models", ReadOnly: true}},
					},
					"general": {Count: 1},
				},
			},
			want: []wantNode{
				controlPlane("kindest/node:v1.33.0"),
				{role: v1alpha4.WorkerRole, image: "kindest/node:v1.33.0", labels: map[string]string{nodeGroupLabel: "general"}, mounts: shared},
				{
					role:   v1alpha4.WorkerRole,
					image:  "kindest/node:v1.32.2",
					labels: map[string]string{nodeGroupLabel: "infra", "dedicated": "infra"},
					mounts: append(slices.Clone(shared), models),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := kindNodes(context.Background(), tt.kindCfg, shared, 8080, 8443)
			if len(nodes) != len(tt.want) {
				t.Fatalf("kindNodes() returned %d nodes, want %d", len(nodes), len(tt.want))
			}

			for i, want := range tt.want {
				n := nodes[i]
				if n.Role != want.role {
					t.Errorf("node %d role = %q, want %q", i, n.Role, want.role)
				}
				if n.Image != want.image {
					t.Errorf("node %d image = %q, want %q", i, n.Image, want.image)
				}
				if !maps.Equal(n.Labels, want.labels) {
					t.Errorf("node %d labels = %v, want %v", i, n.Labels, want.labels)
				}
				// Every node needs the shared mounts: ArgoCD's repo-server
				// reads a file:// GitOps repo from whichever node it lands on.
				if !slices.Equal(n.ExtraMounts, want.mounts) {
					t.Errorf("node %d mounts = %+v, want %+v", i, n.ExtraMounts, want.mounts)
				}
				// A host port can be bound once, so only the control plane
				// publishes the gateway. The NodePorts reach Envoy on any node.
				if n.Role == v1alpha4.ControlPlaneRole {
					if len(n.ExtraPortMappings) != 2 || n.ExtraPortMappings[0].HostPort != 8080 || n.ExtraPortMappings[1].HostPort != 8443 {
						t.Errorf("control plane port mappings = %+v, want the gateway mappings on 8080 and 8443", n.ExtraPortMappings)
					}
				} else if len(n.ExtraPortMappings) != 0 {
					t.Errorf("worker %d has port mappings %+v, want none", i, n.ExtraPortMappings)
				}
			}
		})
	}
}

// TestKindNodesDoesNotAliasMounts guards against nodes sharing one backing
// array: appending a group mount to one worker must not leak onto another
// node that holds the same shared slice.
func TestKindNodesDoesNotAliasMounts(t *testing.T) {
	shared := make([]v1alpha4.Mount, 1, 4)
	shared[0] = v1alpha4.Mount{HostPath: "/gitops", ContainerPath: "/gitops"}
	kindCfg := &KindConfig{NodeGroups: map[string]KindNodeGroup{
		"a": {Count: 1, ExtraMounts: []KindMount{{HostPath: "/a", ContainerPath: "/a"}}},
		"b": {Count: 1, ExtraMounts: []KindMount{{HostPath: "/b", ContainerPath: "/b"}}},
	}}

	nodes := kindNodes(context.Background(), kindCfg, shared, 0, 0)
	if len(nodes) != 3 {
		t.Fatalf("kindNodes() returned %d nodes, want 3", len(nodes))
	}
	if got := nodes[1].ExtraMounts; len(got) != 2 || got[1].ContainerPath != "/a" {
		t.Errorf("group a mounts = %+v, want /gitops and /a", got)
	}
	if got := nodes[2].ExtraMounts; len(got) != 2 || got[1].ContainerPath != "/b" {
		t.Errorf("group b mounts = %+v, want /gitops and /b", got)
	}
	if len(nodes[0].ExtraMounts) != 1 {
		t.Errorf("control plane mounts = %+v, want only /gitops", nodes[0].ExtraMounts)
	}
}

func TestCheckClusterWorkers(t *testing.T) {
	node := func(name string, controlPlane bool) *corev1.Node {
		n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}}}
		if controlPlane {
			n.Labels[controlPlaneLabel] = ""
		}
		return n
	}

	mismatch := []string{"test-project", "node_groups", "recreate", "--regen-apps"}

	tests := []struct {
		name       string
		nodes      []*corev1.Node
		listErr    error
		configured int
		// wantActual is the worker count returned; -1 means it could not be read.
		wantActual int
		// wantWarning lists the substrings of the single expected warning;
		// nil means no warning.
		wantWarning []string
	}{
		{
			name:       "single-node cluster with no workers configured",
			nodes:      []*corev1.Node{node("cp", true)},
			configured: 0,
			wantActual: 0,
		},
		{
			name:       "worker count matches",
			nodes:      []*corev1.Node{node("cp", true), node("w1", false), node("w2", false)},
			configured: 2,
			wantActual: 2,
		},
		{
			name:        "more workers configured than the cluster has",
			nodes:       []*corev1.Node{node("cp", true)},
			configured:  1,
			wantActual:  0,
			wantWarning: mismatch,
		},
		{
			// The cluster's real count is returned, not the configured one,
			// so the caller waits for the nodes that exist.
			name:        "fewer workers configured than the cluster has",
			nodes:       []*corev1.Node{node("cp", true), node("w1", false)},
			configured:  0,
			wantActual:  1,
			wantWarning: mismatch,
		},
		{
			// The check is advisory, so an API failure must not fail the deploy.
			name:        "node list failure warns instead of failing",
			listErr:     errors.New("connection refused"),
			configured:  1,
			wantActual:  -1,
			wantWarning: []string{"test-project", "could not check", "connection refused"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			for _, n := range tt.nodes {
				if _, err := client.CoreV1().Nodes().Create(context.Background(), n, metav1.CreateOptions{}); err != nil {
					t.Fatalf("create node %s: %v", n.Name, err)
				}
			}
			if tt.listErr != nil {
				client.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, tt.listErr
				})
			}
			ch := make(chan status.Update, 10)
			ctx := status.WithChannel(context.Background(), ch)

			if got := checkClusterWorkers(ctx, client, "test-project", tt.configured); got != tt.wantActual {
				t.Errorf("checkClusterWorkers() = %d, want %d", got, tt.wantActual)
			}
			close(ch)

			var warnings []string
			for u := range ch {
				if u.Level == status.LevelWarning {
					warnings = append(warnings, u.Message)
				}
			}
			if tt.wantWarning == nil {
				if len(warnings) != 0 {
					t.Errorf("unexpected warnings: %v", warnings)
				}
				return
			}
			if len(warnings) != 1 {
				t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
			}
			for _, want := range tt.wantWarning {
				if !strings.Contains(warnings[0], want) {
					t.Errorf("warning %q should contain %q", warnings[0], want)
				}
			}
		})
	}
}

// readyNode returns a node with a single Ready condition.
func readyNode(name string, ready bool) *corev1.Node {
	cond := corev1.ConditionFalse
	if ready {
		cond = corev1.ConditionTrue
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: cond},
		}},
	}
}

// nodeListSequence makes the fake clientset answer successive node lists
// from steps, repeating the last step once they run out. A step with err set
// fails that list call.
type nodeListStep struct {
	nodes []*corev1.Node
	err   error
}

func nodeListSequence(client *fake.Clientset, steps []nodeListStep) {
	call := 0
	client.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		step := steps[min(call, len(steps)-1)]
		call++
		if step.err != nil {
			return true, nil, step.err
		}
		list := &corev1.NodeList{}
		for _, n := range step.nodes {
			list.Items = append(list.Items, *n)
		}
		return true, list, nil
	})
}

// fastNodePolling shortens the node poll interval for the test.
func fastNodePolling(t *testing.T) {
	t.Helper()
	orig := nodeReadyPollInterval
	nodeReadyPollInterval = time.Millisecond
	t.Cleanup(func() { nodeReadyPollInterval = orig })
}

func TestWaitForNodesReady(t *testing.T) {
	fastNodePolling(t)
	listErr := errors.New("connection refused")

	tests := []struct {
		name  string
		steps []nodeListStep
		want  int
		// wantErr lists substrings of the timeout error; nil means success.
		wantErr []string
		// notInErr lists substrings the error must not contain.
		notInErr []string
	}{
		{
			name:  "all nodes ready",
			steps: []nodeListStep{{nodes: []*corev1.Node{readyNode("cp", true), readyNode("w1", true)}}},
			want:  2,
		},
		{
			// kind's own ready-wait covers only the control plane, which stays
			// tainted once workers exist, so a Ready control plane alone must
			// not count as a schedulable cluster. The error names the node
			// that is holding the deploy up.
			name:    "control plane ready but worker not ready",
			steps:   []nodeListStep{{nodes: []*corev1.Node{readyNode("cp", true), readyNode("w1", false)}}},
			want:    2,
			wantErr: []string{"test-project", "1 of 2", "not Ready: w1"},
		},
		{
			name:     "worker not registered yet",
			steps:    []nodeListStep{{nodes: []*corev1.Node{readyNode("cp", true)}}},
			want:     2,
			wantErr:  []string{"test-project", "1 of 2", "registered"},
			notInErr: []string{"not Ready:", "node list error"},
		},
		{
			name: "worker becomes ready on a later poll",
			steps: []nodeListStep{
				{nodes: []*corev1.Node{readyNode("cp", true), readyNode("w1", false)}},
				{nodes: []*corev1.Node{readyNode("cp", true), readyNode("w1", true)}},
			},
			want: 2,
		},
		{
			// List errors are transient while nodes join, so polling goes on.
			name: "node list fails, then recovers",
			steps: []nodeListStep{
				{err: listErr},
				{nodes: []*corev1.Node{readyNode("cp", true), readyNode("w1", true)}},
			},
			want: 2,
		},
		{
			// An unreachable or forbidden API must point at the kubeconfig,
			// not at the nodes, so the last list error rides on the timeout.
			name:    "node list keeps failing",
			steps:   []nodeListStep{{err: listErr}},
			want:    2,
			wantErr: []string{"test-project", "0 of 2", "last node list error: connection refused"},
		},
		{
			// A list error followed by a successful list is no longer the
			// cause, so it must not be reported.
			name: "earlier list error is dropped once a list succeeds",
			steps: []nodeListStep{
				{err: listErr},
				{nodes: []*corev1.Node{readyNode("cp", true), readyNode("w1", false)}},
			},
			want:     2,
			wantErr:  []string{"1 of 2", "not Ready: w1"},
			notInErr: []string{"connection refused"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			nodeListSequence(client, tt.steps)

			err := waitForNodesReady(context.Background(), client, "test-project", tt.want, 50*time.Millisecond)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("waitForNodesReady() error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("waitForNodesReady() returned nil, want a timeout error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should contain %q", err.Error(), want)
				}
			}
			for _, unwanted := range tt.notInErr {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("error %q should not contain %q", err.Error(), unwanted)
				}
			}
		})
	}
}

// TestWaitForClusterNodes covers the wait decision Deploy makes after
// creating or reusing a cluster: how many nodes to wait for, and when not to
// wait at all.
func TestWaitForClusterNodes(t *testing.T) {
	fastNodePolling(t)

	controlPlane := func() *corev1.Node {
		n := readyNode("cp", true)
		n.Labels[controlPlaneLabel] = ""
		return n
	}
	worker := readyNode

	tests := []struct {
		name       string
		reused     bool
		configured int
		nodes      []*corev1.Node
		listErr    error
		// wantErr is a substring of the expected error; "" means success.
		wantErr string
		// wantLists is how many node lists the call makes: zero means it
		// neither checked nor waited.
		wantLists int
	}{
		{
			// kind already waited for the lone control plane.
			name:       "created single-node cluster does not wait",
			configured: 0,
			nodes:      []*corev1.Node{controlPlane()},
			wantLists:  0,
		},
		{
			name:       "created cluster waits for its workers",
			configured: 2,
			nodes:      []*corev1.Node{controlPlane(), worker("w1", true), worker("w2", true)},
			wantLists:  1,
		},
		{
			name:       "created cluster fails when a worker never gets Ready",
			configured: 1,
			nodes:      []*corev1.Node{controlPlane(), worker("w1", false)},
			wantErr:    "not Ready: w1",
		},
		{
			name:       "reused single-node cluster only checks the count",
			reused:     true,
			configured: 0,
			nodes:      []*corev1.Node{controlPlane()},
			wantLists:  1,
		},
		{
			name:       "reused cluster waits for its workers",
			reused:     true,
			configured: 1,
			nodes:      []*corev1.Node{controlPlane(), worker("w1", true)},
			wantLists:  2,
		},
		{
			// A count mismatch only warns, so the wait uses the nodes the
			// cluster has. Waiting for the configured 3 would time out.
			name:       "reused cluster with fewer workers than configured waits for the ones it has",
			reused:     true,
			configured: 3,
			nodes:      []*corev1.Node{controlPlane(), worker("w1", true)},
			wantLists:  2,
		},
		{
			// A worker left NotReady (e.g. after a Docker restart) fails the
			// deploy rather than handing ArgoCD a cluster of the wrong shape.
			name:       "reused cluster fails on a stuck worker",
			reused:     true,
			configured: 1,
			nodes:      []*corev1.Node{controlPlane(), worker("w1", false)},
			wantErr:    "not Ready: w1",
		},
		{
			// The count check is advisory, so a failed list skips the wait
			// instead of failing the deploy.
			name:       "reused cluster whose nodes cannot be listed does not wait",
			reused:     true,
			configured: 1,
			listErr:    errors.New("connection refused"),
			wantLists:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			lists := 0
			client.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
				lists++
				if tt.listErr != nil {
					return true, nil, tt.listErr
				}
				list := &corev1.NodeList{}
				for _, n := range tt.nodes {
					list.Items = append(list.Items, *n)
				}
				return true, list, nil
			})

			err := waitForClusterNodes(context.Background(), client, "test-project", tt.configured, tt.reused, 50*time.Millisecond)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("waitForClusterNodes() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("waitForClusterNodes() error: %v", err)
			}
			if lists != tt.wantLists {
				t.Errorf("waitForClusterNodes() listed nodes %d times, want %d", lists, tt.wantLists)
			}
		})
	}
}
