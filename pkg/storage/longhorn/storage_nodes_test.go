package longhorn

import (
	"context"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

func TestStorageSelector(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want map[string]string
	}{
		{"nil config defaults to storage label", nil, map[string]string{"node.longhorn.io/storage": "true"}},
		{"no selector defaults to storage label", &Config{}, map[string]string{"node.longhorn.io/storage": "true"}},
		{"custom selector is returned", &Config{NodeSelector: map[string]string{"pool": "lh"}}, map[string]string{"pool": "lh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StorageSelector(tt.cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("StorageSelector() = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("StorageSelector()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestStorageNodeLabels(t *testing.T) {
	// Always includes the literal create-default-disk label wire value, plus the
	// selector labels.
	got := StorageNodeLabels(&Config{NodeSelector: map[string]string{"pool": "lh"}})
	if got["node.longhorn.io/create-default-disk"] != "true" {
		t.Errorf("StorageNodeLabels missing create-default-disk=true, got %v", got)
	}
	if got["pool"] != "lh" {
		t.Errorf("StorageNodeLabels missing selector label pool=lh, got %v", got)
	}

	def := StorageNodeLabels(nil)
	if def["node.longhorn.io/create-default-disk"] != "true" || def["node.longhorn.io/storage"] != "true" {
		t.Errorf("StorageNodeLabels(nil) = %v, want default storage + create-default-disk labels", def)
	}
}

func TestWarnIfMissingStorageDiskLabel(t *testing.T) {
	node := func(name string, labels map[string]string) runtime.Object {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	}
	tests := []struct {
		name     string
		nodes    []runtime.Object
		wantWarn bool
	}{
		{
			name:     "no node carries the disk label warns",
			nodes:    []runtime.Object{node("a", map[string]string{"node.longhorn.io/storage": "true"})},
			wantWarn: true,
		},
		{
			name:     "a node with the disk label stays quiet",
			nodes:    []runtime.Object{node("a", map[string]string{"node.longhorn.io/create-default-disk": "true"})},
			wantWarn: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.nodes...)

			var mu sync.Mutex
			var warned bool
			ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
				if u.Level == status.LevelWarning && strings.Contains(u.Message, "create-default-disk") {
					mu.Lock()
					warned = true
					mu.Unlock()
				}
			})

			if err := warnIfMissingStorageDiskLabelWithClient(ctx, client, &Config{DedicatedNodes: true}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			cleanup()

			mu.Lock()
			defer mu.Unlock()
			if warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v", warned, tt.wantWarn)
			}
		})
	}
}
