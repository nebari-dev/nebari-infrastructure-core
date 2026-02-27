package aws

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLonghornHelmValues(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		checkValues map[string]any // nested key checks
	}{
		{
			name:   "default config produces base values",
			config: &Config{},
			checkValues: map[string]any{
				"persistence.defaultClassReplicaCount":        2,
				"persistence.defaultFsType":                   "ext4",
				"persistence.defaultClass":                    true,
				"defaultSettings.replicaZoneSoftAntiAffinity": "true",
				"defaultSettings.replicaAutoBalance":          "best-effort",
			},
		},
		{
			name: "custom replica count",
			config: &Config{
				Longhorn: &LonghornConfig{ReplicaCount: 3},
			},
			checkValues: map[string]any{
				"persistence.defaultClassReplicaCount": 3,
			},
		},
		{
			name: "dedicated nodes adds nodeSelector and tolerations",
			config: &Config{
				Longhorn: &LonghornConfig{
					DedicatedNodes: true,
					NodeSelector:   map[string]string{"node.longhorn.io/storage": "true"},
				},
			},
			checkValues: map[string]any{
				"defaultSettings.createDefaultDiskLabeledNodes": true,
			},
		},
		{
			name: "dedicated nodes without custom nodeSelector uses default",
			config: &Config{
				Longhorn: &LonghornConfig{
					DedicatedNodes: true,
				},
			},
			checkValues: map[string]any{
				"defaultSettings.createDefaultDiskLabeledNodes": true,
			},
		},
		{
			name: "non-dedicated nodes omits nodeSelector and tolerations",
			config: &Config{
				Longhorn: &LonghornConfig{
					DedicatedNodes: false,
					ReplicaCount:   2,
				},
			},
			checkValues: map[string]any{
				"persistence.defaultClassReplicaCount": 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := longhornHelmValues(tt.config)

			for key, want := range tt.checkValues {
				got := getNestedValue(values, key)
				if got == nil {
					t.Errorf("key %q not found in values", key)
					continue
				}
				if got != want {
					t.Errorf("values[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
				}
			}
		})
	}
}

func TestLonghornHelmValuesDedicatedNodesStructure(t *testing.T) {
	cfg := &Config{
		Longhorn: &LonghornConfig{
			DedicatedNodes: true,
			NodeSelector:   map[string]string{"node.longhorn.io/storage": "true"},
		},
	}

	values := longhornHelmValues(cfg)

	// Check longhornManager has nodeSelector
	manager, ok := values["longhornManager"].(map[string]any)
	if !ok {
		t.Fatal("longhornManager not found or not a map")
	}
	ns, ok := manager["nodeSelector"].(map[string]string)
	if !ok {
		t.Fatal("longhornManager.nodeSelector not found or not a map[string]string")
	}
	if ns["node.longhorn.io/storage"] != "true" {
		t.Errorf("longhornManager.nodeSelector[node.longhorn.io/storage] = %q, want %q", ns["node.longhorn.io/storage"], "true")
	}

	// Check longhornManager has tolerations
	tolerations, ok := manager["tolerations"].([]map[string]string)
	if !ok {
		t.Fatal("longhornManager.tolerations not found or not a []map[string]string")
	}
	if len(tolerations) != 1 {
		t.Fatalf("longhornManager.tolerations length = %d, want 1", len(tolerations))
	}
	if tolerations[0]["key"] != "node.longhorn.io/storage" {
		t.Errorf("toleration key = %q, want %q", tolerations[0]["key"], "node.longhorn.io/storage")
	}
	if tolerations[0]["operator"] != "Exists" {
		t.Errorf("toleration operator = %q, want %q", tolerations[0]["operator"], "Exists")
	}
	if tolerations[0]["effect"] != "NoSchedule" {
		t.Errorf("toleration effect = %q, want %q", tolerations[0]["effect"], "NoSchedule")
	}

	// Check longhornDriver has the same structure
	driver, ok := values["longhornDriver"].(map[string]any)
	if !ok {
		t.Fatal("longhornDriver not found or not a map")
	}
	_, ok = driver["nodeSelector"].(map[string]string)
	if !ok {
		t.Fatal("longhornDriver.nodeSelector not found or not a map[string]string")
	}
	_, ok = driver["tolerations"].([]map[string]string)
	if !ok {
		t.Fatal("longhornDriver.tolerations not found or not a []map[string]string")
	}
}

func TestLonghornHelmValuesNonDedicatedOmitsNodeSelector(t *testing.T) {
	cfg := &Config{
		Longhorn: &LonghornConfig{
			DedicatedNodes: false,
		},
	}

	values := longhornHelmValues(cfg)

	if _, ok := values["longhornManager"]; ok {
		t.Error("longhornManager should not be set when DedicatedNodes is false")
	}
	if _, ok := values["longhornDriver"]; ok {
		t.Error("longhornDriver should not be set when DedicatedNodes is false")
	}
}

func TestEnsureNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		existing  []runtime.Object
	}{
		{
			name:      "creates namespace when it does not exist",
			namespace: longhornNamespace,
			existing:  nil,
		},
		{
			name:      "succeeds when namespace already exists",
			namespace: longhornNamespace,
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: longhornNamespace},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...) //nolint:staticcheck

			err := ensureNamespace(context.Background(), client, tt.namespace)
			if err != nil {
				t.Fatalf("ensureNamespace() error = %v", err)
			}

			ns, err := client.CoreV1().Namespaces().Get(context.Background(), tt.namespace, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("namespace %s not found after ensureNamespace: %v", tt.namespace, err)
			}
			if ns.Name != tt.namespace {
				t.Errorf("namespace name = %q, want %q", ns.Name, tt.namespace)
			}
		})
	}
}

func TestEnsureISCSIWithClient(t *testing.T) {
	tests := []struct {
		name          string
		existing      []runtime.Object
		wantErr       bool
		checkDSExists bool
		checkNSExists bool
	}{
		{
			name:          "creates namespace and DaemonSet when neither exists",
			existing:      nil,
			wantErr:       true, // DaemonSet status won't update with fake client
			checkDSExists: true,
			checkNSExists: true,
		},
		{
			name: "creates DaemonSet when namespace already exists",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: longhornNamespace},
				},
			},
			wantErr:       true, // DaemonSet status won't update with fake client
			checkDSExists: true,
			checkNSExists: true,
		},
		{
			name: "updates DaemonSet when it already exists",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: longhornNamespace},
				},
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "longhorn-iscsi-installation",
						Namespace: longhornNamespace,
					},
					Spec: appsv1.DaemonSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "longhorn-iscsi-installation"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "longhorn-iscsi-installation"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "sleep", Image: "registry.k8s.io/pause:3.1"},
								},
							},
						},
					},
				},
			},
			// The fake client doesn't simulate the DaemonSet controller,
			// so status won't be updated after the Update call. Readiness
			// polling is tested separately in TestWaitForDaemonSetReady.
			wantErr:       true,
			checkDSExists: true,
			checkNSExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...) //nolint:staticcheck

			// Use a short context timeout so tests that expect a timeout
			// don't block for the full iscsiDaemonSetTimeout (3 min).
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := ensureISCSIWithClient(ctx, client)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error (timeout), got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			// Use a fresh context for verification since the test context may be expired
			verifyCtx := context.Background()

			if tt.checkNSExists {
				ns, nsErr := client.CoreV1().Namespaces().Get(verifyCtx, longhornNamespace, metav1.GetOptions{})
				if nsErr != nil {
					t.Fatalf("namespace %s not found: %v", longhornNamespace, nsErr)
				}
				if ns.Name != longhornNamespace {
					t.Errorf("namespace name = %q, want %q", ns.Name, longhornNamespace)
				}
			}

			if tt.checkDSExists {
				ds, dsErr := client.AppsV1().DaemonSets(longhornNamespace).Get(verifyCtx, "longhorn-iscsi-installation", metav1.GetOptions{})
				if dsErr != nil {
					t.Fatalf("DaemonSet not found: %v", dsErr)
				}
				if ds.Name != "longhorn-iscsi-installation" {
					t.Errorf("DaemonSet name = %q, want %q", ds.Name, "longhorn-iscsi-installation")
				}
				if ds.Namespace != longhornNamespace {
					t.Errorf("DaemonSet namespace = %q, want %q", ds.Namespace, longhornNamespace)
				}
			}
		})
	}
}

func TestWaitForDaemonSetReady(t *testing.T) {
	tests := []struct {
		name    string
		ds      *appsv1.DaemonSet
		wantErr bool
	}{
		{
			name: "returns immediately when DaemonSet is ready",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ds",
					Namespace: longhornNamespace,
				},
				Status: appsv1.DaemonSetStatus{
					DesiredNumberScheduled: 3,
					NumberReady:            3,
				},
			},
			wantErr: false,
		},
		{
			name: "times out when DaemonSet is not ready",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ds",
					Namespace: longhornNamespace,
				},
				Status: appsv1.DaemonSetStatus{
					DesiredNumberScheduled: 3,
					NumberReady:            1,
				},
			},
			wantErr: true,
		},
		{
			name: "times out when DesiredNumberScheduled is zero",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ds",
					Namespace: longhornNamespace,
				},
				Status: appsv1.DaemonSetStatus{
					DesiredNumberScheduled: 0,
					NumberReady:            0,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: longhornNamespace}}
			client := fake.NewSimpleClientset(ns, tt.ds) //nolint:staticcheck

			err := waitForDaemonSetReady(context.Background(), kubernetes.Interface(client), longhornNamespace, tt.ds.Name, 1*time.Second)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected timeout error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// getNestedValue retrieves a value from a nested map using a dot-separated path.
func getNestedValue(m map[string]any, path string) any {
	parts := splitDotPath(path)
	var current any = m
	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = cm[part]
		if !ok {
			return nil
		}
	}
	return current
}

// splitDotPath splits a dot-separated path into parts.
func splitDotPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	parts = append(parts, path[start:])
	return parts
}
