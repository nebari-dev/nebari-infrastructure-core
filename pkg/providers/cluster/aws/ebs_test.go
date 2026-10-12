package aws

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

func TestGP3StorageClass(t *testing.T) {
	sc := gp3StorageClass()

	if sc.Name != storageClassGP3 {
		t.Errorf("Name = %q, want %q", sc.Name, storageClassGP3)
	}
	if sc.Provisioner != ebsCSIProvisioner {
		t.Errorf("Provisioner = %q, want %q", sc.Provisioner, ebsCSIProvisioner)
	}
	if got := sc.Parameters["type"]; got != "gp3" {
		t.Errorf("Parameters[type] = %q, want gp3", got)
	}
	if got := sc.Parameters["encrypted"]; got != "true" {
		t.Errorf("Parameters[encrypted] = %q, want true", got)
	}
	if sc.VolumeBindingMode == nil || *sc.VolumeBindingMode != storagev1.VolumeBindingWaitForFirstConsumer {
		t.Errorf("VolumeBindingMode = %v, want WaitForFirstConsumer", sc.VolumeBindingMode)
	}
	if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
		t.Errorf("AllowVolumeExpansion = %v, want true", sc.AllowVolumeExpansion)
	}
	if sc.ReclaimPolicy == nil || *sc.ReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
		t.Errorf("ReclaimPolicy = %v, want Delete", sc.ReclaimPolicy)
	}
	if _, ok := sc.Annotations[storageclass.DefaultAnnotation]; ok {
		t.Error("gp3StorageClass() must not set the default annotation; MarkDefaultIfNone decides that")
	}
}

func TestEnsureGP3StorageClassWithClient(t *testing.T) {
	withDefault := func(sc *storagev1.StorageClass, value string) *storagev1.StorageClass {
		sc.Annotations = map[string]string{storageclass.DefaultAnnotation: value}
		return sc
	}
	// EKS ships an in-tree gp2 class with no default annotation (1.30+).
	gp2 := func() *storagev1.StorageClass {
		return &storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: "gp2"},
			Provisioner: "kubernetes.io/aws-ebs",
			Parameters:  map[string]string{"type": "gp2"},
		}
	}

	tests := []struct {
		name             string
		existing         []runtime.Object
		wantGP3Default   bool
		wantOtherDefault map[string]bool
		wantProvisioner  string
	}{
		{
			name:             "creates gp3 and marks it default on a fresh EKS cluster",
			existing:         []runtime.Object{gp2()},
			wantGP3Default:   true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
		},
		{
			name:             "creates gp3 but leaves an admin-chosen default alone",
			existing:         []runtime.Object{withDefault(gp2(), "true")},
			wantGP3Default:   false,
			wantOtherDefault: map[string]bool{"gp2": true},
			wantProvisioner:  ebsCSIProvisioner,
		},
		{
			name: "creates gp3 non-default when Longhorn is already the default",
			existing: []runtime.Object{
				gp2(),
				withDefault(&storagev1.StorageClass{
					ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
					Provisioner: "driver.longhorn.io",
				}, "true"),
			},
			wantGP3Default:   false,
			wantOtherDefault: map[string]bool{"longhorn": true, "gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
		},
		{
			name:             "is idempotent when gp3 already exists and is default",
			existing:         []runtime.Object{gp2(), withDefault(gp3StorageClass(), "true")},
			wantGP3Default:   true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
		},
		{
			name: "leaves a hand-made gp3 class unchanged and marks it default",
			existing: []runtime.Object{
				gp2(),
				&storagev1.StorageClass{
					ObjectMeta:  metav1.ObjectMeta{Name: storageClassGP3},
					Provisioner: "kubernetes.io/aws-ebs",
					Parameters:  map[string]string{"type": "gp3"},
				},
			},
			wantGP3Default:   true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  "kubernetes.io/aws-ebs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...)
			if err := ensureGP3StorageClassWithClient(context.Background(), client); err != nil {
				t.Fatalf("ensureGP3StorageClassWithClient() error = %v", err)
			}

			got, err := client.StorageV1().StorageClasses().Get(context.Background(), storageClassGP3, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("gp3 StorageClass not found: %v", err)
			}
			if got.Provisioner != tt.wantProvisioner {
				t.Errorf("Provisioner = %q, want %q", got.Provisioner, tt.wantProvisioner)
			}
			if isDefault := got.Annotations[storageclass.DefaultAnnotation] == "true"; isDefault != tt.wantGP3Default {
				t.Errorf("gp3 default = %v, want %v", isDefault, tt.wantGP3Default)
			}

			for name, wantDefault := range tt.wantOtherDefault {
				other, err := client.StorageV1().StorageClasses().Get(context.Background(), name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("StorageClass %q not found: %v", name, err)
				}
				if isDefault := other.Annotations[storageclass.DefaultAnnotation] == "true"; isDefault != wantDefault {
					t.Errorf("StorageClass %q default = %v, want %v", name, isDefault, wantDefault)
				}
			}
		})
	}
}
