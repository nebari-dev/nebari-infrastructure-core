package storageclass

import (
	"context"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func sc(name string, isDefault string) *storagev1.StorageClass {
	annotations := map[string]string{}
	if isDefault != "" {
		annotations[DefaultAnnotation] = isDefault
	}
	return &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: name, Annotations: annotations},
		Provisioner: "test-provisioner",
	}
}

func betaDefault(s *storagev1.StorageClass) *storagev1.StorageClass {
	s.Annotations[BetaDefaultAnnotation] = "true"
	return s
}

func assertDefaults(t *testing.T, client kubernetes.Interface, want map[string]bool) {
	t.Helper()
	for name, wantDefault := range want {
		got, err := client.StorageV1().StorageClasses().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("StorageClass %q not found: %v", name, err)
		}
		isDefault := IsDefault(got)
		if isDefault != wantDefault {
			t.Errorf("StorageClass %q default=%v, want %v (annotations=%v)", name, isDefault, wantDefault, got.Annotations)
		}
	}
}

func TestEnsureSoleDefault(t *testing.T) {
	tests := []struct {
		name        string
		existing    []runtime.Object
		keep        string
		wantDefault map[string]bool // SC name -> still default?
	}{
		{
			name: "demotes previous default StorageClass",
			existing: []runtime.Object{
				sc("hcloud-volumes", "true"),
				sc("longhorn", "true"),
			},
			keep: "longhorn",
			wantDefault: map[string]bool{
				"hcloud-volumes": false,
				"longhorn":       true,
			},
		},
		{
			name: "no-op when kept SC is the only default",
			existing: []runtime.Object{
				sc("longhorn", "true"),
				sc("local-path", ""),
			},
			keep: "longhorn",
			wantDefault: map[string]bool{
				"longhorn":   true,
				"local-path": false,
			},
		},
		{
			name: "demotes multiple stale defaults",
			existing: []runtime.Object{
				sc("hcloud-volumes", "true"),
				sc("vsphere-thin", "true"),
				sc("longhorn", "true"),
			},
			keep: "longhorn",
			wantDefault: map[string]bool{
				"hcloud-volumes": false,
				"vsphere-thin":   false,
				"longhorn":       true,
			},
		},
		{
			name:        "no StorageClasses is not an error",
			existing:    nil,
			keep:        "longhorn",
			wantDefault: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...)
			if err := EnsureSoleDefault(context.Background(), client, tt.keep); err != nil {
				t.Fatalf("EnsureSoleDefault() error = %v", err)
			}
			assertDefaults(t, client, tt.wantDefault)
		})
	}
}

func TestMarkDefaultIfNone(t *testing.T) {
	tests := []struct {
		name        string
		existing    []runtime.Object
		target      string
		want        bool
		wantErr     bool
		wantDefault map[string]bool
		wantPatch   bool // the call patches the target's annotation
	}{
		{
			name: "marks target when no default exists",
			existing: []runtime.Object{
				sc("gp2", ""),
				sc("gp3", ""),
			},
			target:    "gp3",
			want:      true,
			wantPatch: true,
			wantDefault: map[string]bool{
				"gp2": false,
				"gp3": true,
			},
		},
		{
			name: "marks target when another class is explicitly non-default",
			existing: []runtime.Object{
				sc("gp2", "false"),
				sc("gp3", ""),
			},
			target:    "gp3",
			want:      true,
			wantPatch: true,
			wantDefault: map[string]bool{
				"gp2": false,
				"gp3": true,
			},
		},
		{
			name: "leaves an existing default alone",
			existing: []runtime.Object{
				sc("admin-class", "true"),
				sc("gp3", ""),
			},
			target: "gp3",
			want:   false,
			wantDefault: map[string]bool{
				"admin-class": true,
				"gp3":         false,
			},
		},
		{
			name: "leaves an existing default set only by the beta annotation alone",
			existing: []runtime.Object{
				betaDefault(sc("legacy-class", "")),
				sc("gp3", ""),
			},
			target: "gp3",
			want:   false,
			wantDefault: map[string]bool{
				"legacy-class": true,
				"gp3":          false,
			},
		},
		{
			name: "no-op when target is already the default under the beta annotation",
			existing: []runtime.Object{
				sc("gp2", ""),
				betaDefault(sc("gp3", "")),
			},
			target: "gp3",
			want:   true,
			wantDefault: map[string]bool{
				"gp2": false,
				"gp3": true,
			},
		},
		{
			name: "no-op when target is already the default",
			existing: []runtime.Object{
				sc("gp2", ""),
				sc("gp3", "true"),
			},
			target: "gp3",
			want:   true,
			wantDefault: map[string]bool{
				"gp2": false,
				"gp3": true,
			},
		},
		{
			name: "errors when target does not exist",
			existing: []runtime.Object{
				sc("gp2", ""),
			},
			target:  "gp3",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...)
			got, err := MarkDefaultIfNone(context.Background(), client, tt.target)
			if tt.wantErr {
				if err == nil {
					t.Fatal("MarkDefaultIfNone() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("MarkDefaultIfNone() error = %v", err)
			}
			patched := false
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" {
					patched = true
				}
			}
			if patched != tt.wantPatch {
				t.Errorf("patch action recorded = %v, want %v", patched, tt.wantPatch)
			}
			if got != tt.want {
				t.Errorf("MarkDefaultIfNone() = %v, want %v", got, tt.want)
			}
			assertDefaults(t, client, tt.wantDefault)
		})
	}
}
