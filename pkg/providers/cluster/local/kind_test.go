package local

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	mounts := []v1alpha4.Mount{
		{HostPath: "/gitops", ContainerPath: "/gitops", Readonly: true},
		{HostPath: "/data", ContainerPath: "/data"},
	}

	tests := []struct {
		name        string
		workers     int
		wantWorkers int
	}{
		{name: "no workers keeps the single-node cluster", workers: 0, wantWorkers: 0},
		{name: "one worker", workers: 1, wantWorkers: 1},
		{name: "several workers", workers: 3, wantWorkers: 3},
		{name: "negative count adds no workers", workers: -1, wantWorkers: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := kindNodes(context.Background(), mounts, tt.workers, 8080, 8443)
			if len(nodes) != 1+tt.wantWorkers {
				t.Fatalf("kindNodes() returned %d nodes, want %d", len(nodes), 1+tt.wantWorkers)
			}

			cp := nodes[0]
			if cp.Role != v1alpha4.ControlPlaneRole {
				t.Errorf("first node role = %q, want %q", cp.Role, v1alpha4.ControlPlaneRole)
			}
			if len(cp.ExtraPortMappings) != 2 || cp.ExtraPortMappings[0].HostPort != 8080 || cp.ExtraPortMappings[1].HostPort != 8443 {
				t.Errorf("control plane port mappings = %+v, want the gateway mappings on 8080 and 8443", cp.ExtraPortMappings)
			}
			if len(cp.ExtraMounts) != len(mounts) {
				t.Errorf("control plane has %d mounts, want %d", len(cp.ExtraMounts), len(mounts))
			}

			for i, w := range nodes[1:] {
				if w.Role != v1alpha4.WorkerRole {
					t.Errorf("node %d role = %q, want %q", i+1, w.Role, v1alpha4.WorkerRole)
				}
				// A host port can be bound once, so only the control plane
				// publishes the gateway. The NodePorts reach Envoy on any node.
				if len(w.ExtraPortMappings) != 0 {
					t.Errorf("worker %d has port mappings %+v, want none", i+1, w.ExtraPortMappings)
				}
				// Every node needs the mounts: ArgoCD's repo-server reads a
				// file:// GitOps repo from whichever node it lands on.
				if len(w.ExtraMounts) != len(mounts) {
					t.Fatalf("worker %d has %d mounts, want %d", i+1, len(w.ExtraMounts), len(mounts))
				}
				for j, m := range w.ExtraMounts {
					if m != mounts[j] {
						t.Errorf("worker %d mount %d = %+v, want %+v", i+1, j, m, mounts[j])
					}
				}
			}
		})
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

	mismatch := []string{"test-project", "workers", "recreate"}

	tests := []struct {
		name       string
		nodes      []*corev1.Node
		listErr    error
		configured int
		// wantWarning lists the substrings of the single expected warning;
		// nil means no warning.
		wantWarning []string
	}{
		{
			name:       "single-node cluster with no workers configured",
			nodes:      []*corev1.Node{node("cp", true)},
			configured: 0,
		},
		{
			name:       "worker count matches",
			nodes:      []*corev1.Node{node("cp", true), node("w1", false), node("w2", false)},
			configured: 2,
		},
		{
			name:        "more workers configured than the cluster has",
			nodes:       []*corev1.Node{node("cp", true)},
			configured:  1,
			wantWarning: mismatch,
		},
		{
			name:        "fewer workers configured than the cluster has",
			nodes:       []*corev1.Node{node("cp", true), node("w1", false)},
			configured:  0,
			wantWarning: mismatch,
		},
		{
			// The check is advisory, so an API failure must not fail the deploy.
			name:        "node list failure warns instead of failing",
			listErr:     errors.New("connection refused"),
			configured:  1,
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

			checkClusterWorkers(ctx, client, "test-project", tt.configured)
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
