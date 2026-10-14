package aws

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

func intPtr(v int) *int { return &v }

func TestEBSDefaults(t *testing.T) {
	tests := []struct {
		name     string
		ebs      *EBSConfig
		wantType string
		wantName string
	}{
		{name: "no ebs block", ebs: nil, wantType: "gp3", wantName: "gp3"},
		{name: "empty ebs block", ebs: &EBSConfig{}, wantType: "gp3", wantName: "gp3"},
		{name: "name follows type", ebs: &EBSConfig{Type: "io2"}, wantType: "io2", wantName: "io2"},
		{name: "gp2 resolves to the EKS gp2 class", ebs: &EBSConfig{Type: "gp2"}, wantType: "gp2", wantName: "gp2"},
		{name: "explicit name", ebs: &EBSConfig{StorageClassName: "fast"}, wantType: "gp3", wantName: "fast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{EBS: tt.ebs}
			if got := c.EBSVolumeType(); got != tt.wantType {
				t.Errorf("EBSVolumeType() = %q, want %q", got, tt.wantType)
			}
			if got := c.EBSStorageClassName(); got != tt.wantName {
				t.Errorf("EBSStorageClassName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestValidateEBSConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{name: "no ebs block", cfg: &Config{}},
		{name: "gp3 with iops and throughput", cfg: &Config{EBS: &EBSConfig{Type: "gp3", IOPS: intPtr(6000), Throughput: intPtr(250)}}},
		{name: "gp2", cfg: &Config{EBS: &EBSConfig{Type: "gp2"}}},
		{name: "io2 with iops", cfg: &Config{EBS: &EBSConfig{Type: "io2", IOPS: intPtr(10000)}}},
		{name: "st1", cfg: &Config{EBS: &EBSConfig{Type: "st1"}}},
		{name: "kms key", cfg: &Config{EBS: &EBSConfig{KMSKeyARN: testCMKARN}}},
		{name: "unknown type", cfg: &Config{EBS: &EBSConfig{Type: "standard"}}, wantErr: "ebs.type"},
		{name: "gp3 iops below baseline", cfg: &Config{EBS: &EBSConfig{IOPS: intPtr(1000)}}, wantErr: "baseline"},
		{name: "gp3 throughput below baseline", cfg: &Config{EBS: &EBSConfig{Throughput: intPtr(100)}}, wantErr: "baseline"},
		{name: "iops on gp2", cfg: &Config{EBS: &EBSConfig{Type: "gp2", IOPS: intPtr(3000)}}, wantErr: "ebs.iops: not supported"},
		{name: "io1 without iops", cfg: &Config{EBS: &EBSConfig{Type: "io1"}}, wantErr: "ebs.iops: required"},
		{name: "io2 zero iops", cfg: &Config{EBS: &EBSConfig{Type: "io2", IOPS: intPtr(0)}}, wantErr: "must be positive"},
		{name: "throughput on io2", cfg: &Config{EBS: &EBSConfig{Type: "io2", IOPS: intPtr(1000), Throughput: intPtr(500)}}, wantErr: "ebs.throughput: not supported"},
		{name: "kms alias", cfg: &Config{EBS: &EBSConfig{KMSKeyARN: "arn:aws:kms:us-west-2:123456789012:alias/ebs"}}, wantErr: "ebs.kms_key_arn"},
		{name: "invalid class name", cfg: &Config{EBS: &EBSConfig{StorageClassName: "Fast_SSD"}}, wantErr: "not a valid StorageClass name"},
		{name: "longhorn name", cfg: &Config{EBS: &EBSConfig{StorageClassName: "longhorn"}}, wantErr: "reserved for Longhorn"},
		{
			name:    "efs name clash",
			cfg:     &Config{EFS: &EFSConfig{Enabled: true}, EBS: &EBSConfig{StorageClassName: defaultEFSStorageClassName}},
			wantErr: "EFS StorageClass",
		},
		{
			name: "efs name with efs disabled",
			cfg:  &Config{EFS: &EFSConfig{Enabled: false}, EBS: &EBSConfig{StorageClassName: defaultEFSStorageClassName}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEBSConfig(tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateEBSConfig() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateEBSConfig() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestEBSStorageClass(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *Config
		wantName   string
		wantParams map[string]string
	}{
		{
			name:       "defaults to gp3",
			cfg:        &Config{},
			wantName:   "gp3",
			wantParams: map[string]string{"type": "gp3", "encrypted": "true"},
		},
		{
			name: "gp3 with performance and kms",
			cfg: &Config{EBS: &EBSConfig{
				IOPS: intPtr(6000), Throughput: intPtr(250), KMSKeyARN: testCMKARN, StorageClassName: "gp3-fast",
			}},
			wantName: "gp3-fast",
			wantParams: map[string]string{
				"type": "gp3", "encrypted": "true", "iops": "6000", "throughput": "250", "kmsKeyId": testCMKARN,
			},
		},
		{
			name:       "io2",
			cfg:        &Config{EBS: &EBSConfig{Type: "io2", IOPS: intPtr(10000)}},
			wantName:   "io2",
			wantParams: map[string]string{"type": "io2", "encrypted": "true", "iops": "10000"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := ebsStorageClass(tt.cfg)

			if sc.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", sc.Name, tt.wantName)
			}
			if !maps.Equal(sc.Parameters, tt.wantParams) {
				t.Errorf("Parameters = %v, want %v", sc.Parameters, tt.wantParams)
			}
			if sc.Provisioner != ebsCSIProvisioner {
				t.Errorf("Provisioner = %q, want %q", sc.Provisioner, ebsCSIProvisioner)
			}
			if sc.Labels[managedByLabel] != managedByValue {
				t.Errorf("Labels[%s] = %q, want %q", managedByLabel, sc.Labels[managedByLabel], managedByValue)
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
				t.Error("ebsStorageClass() must not set the default annotation; MarkDefaultIfNone decides that")
			}
		})
	}
}

func TestEBSCSIKMSKeyARNs(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want []string
	}{
		{name: "none", cfg: &Config{}, want: nil},
		{name: "list only", cfg: &Config{EBSKMSKeyARNs: []string{testOtherARN}}, want: []string{testOtherARN}},
		{name: "class key only", cfg: &Config{EBS: &EBSConfig{KMSKeyARN: testCMKARN}}, want: []string{testCMKARN}},
		{
			name: "class key added to list",
			cfg:  &Config{EBSKMSKeyARNs: []string{testOtherARN}, EBS: &EBSConfig{KMSKeyARN: testCMKARN}},
			want: []string{testOtherARN, testCMKARN},
		},
		{
			name: "class key already listed",
			cfg:  &Config{EBSKMSKeyARNs: []string{testCMKARN}, EBS: &EBSConfig{KMSKeyARN: testCMKARN}},
			want: []string{testCMKARN},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ebsCSIKMSKeyARNs(); !slices.Equal(got, tt.want) {
				t.Errorf("ebsCSIKMSKeyARNs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureEBSStorageClassWithClient(t *testing.T) {
	withDefault := func(sc *storagev1.StorageClass, value string) *storagev1.StorageClass {
		sc.Annotations = map[string]string{storageclass.DefaultAnnotation: value}
		return sc
	}
	// EKS ships an in-tree gp2 class with no default annotation (1.30+).
	gp2 := func() *storagev1.StorageClass {
		return &storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: "gp2"},
			Provisioner: ebsInTreeProvisioner,
			Parameters:  map[string]string{"type": "gp2", "fsType": "ext4"},
		}
	}
	gp3Fast := &Config{EBS: &EBSConfig{IOPS: intPtr(6000)}}

	tests := []struct {
		name             string
		cfg              *Config
		existing         []runtime.Object
		wantClass        string
		wantDefault      bool
		wantOtherDefault map[string]bool
		wantProvisioner  string
		wantParams       map[string]string
		wantLabel        bool
	}{
		{
			name:             "creates gp3 and marks it default on a fresh EKS cluster",
			cfg:              &Config{},
			existing:         []runtime.Object{gp2()},
			wantClass:        "gp3",
			wantDefault:      true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
			wantParams:       map[string]string{"type": "gp3", "encrypted": "true"},
			wantLabel:        true,
		},
		{
			name:             "creates gp3 but leaves an admin-chosen default alone",
			cfg:              &Config{},
			existing:         []runtime.Object{withDefault(gp2(), "true")},
			wantClass:        "gp3",
			wantDefault:      false,
			wantOtherDefault: map[string]bool{"gp2": true},
			wantProvisioner:  ebsCSIProvisioner,
			wantParams:       map[string]string{"type": "gp3", "encrypted": "true"},
			wantLabel:        true,
		},
		{
			name: "creates gp3 non-default when Longhorn is already the default",
			cfg:  &Config{},
			existing: []runtime.Object{
				gp2(),
				withDefault(&storagev1.StorageClass{
					ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
					Provisioner: "driver.longhorn.io",
				}, "true"),
			},
			wantClass:        "gp3",
			wantDefault:      false,
			wantOtherDefault: map[string]bool{"longhorn": true, "gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
			wantParams:       map[string]string{"type": "gp3", "encrypted": "true"},
			wantLabel:        true,
		},
		{
			name:             "is idempotent when gp3 already exists and is default",
			cfg:              &Config{},
			existing:         []runtime.Object{gp2(), withDefault(ebsStorageClass(&Config{}), "true")},
			wantClass:        "gp3",
			wantDefault:      true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
			wantParams:       map[string]string{"type": "gp3", "encrypted": "true"},
			wantLabel:        true,
		},
		{
			name:             "recreates its own class when the ebs config changes and keeps it default",
			cfg:              gp3Fast,
			existing:         []runtime.Object{gp2(), withDefault(ebsStorageClass(&Config{}), "true")},
			wantClass:        "gp3",
			wantDefault:      true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
			wantParams:       map[string]string{"type": "gp3", "encrypted": "true", "iops": "6000"},
			wantLabel:        true,
		},
		{
			name: "leaves a hand-made gp3 class unchanged and marks it default",
			cfg:  gp3Fast,
			existing: []runtime.Object{
				gp2(),
				&storagev1.StorageClass{
					ObjectMeta:  metav1.ObjectMeta{Name: "gp3"},
					Provisioner: ebsInTreeProvisioner,
					Parameters:  map[string]string{"type": "gp3"},
				},
			},
			wantClass:        "gp3",
			wantDefault:      true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsInTreeProvisioner,
			wantParams:       map[string]string{"type": "gp3"},
			wantLabel:        false,
		},
		{
			name:            "type gp2 reuses the EKS gp2 class and marks it default",
			cfg:             &Config{EBS: &EBSConfig{Type: "gp2"}},
			existing:        []runtime.Object{gp2()},
			wantClass:       "gp2",
			wantDefault:     true,
			wantProvisioner: ebsInTreeProvisioner,
			wantParams:      map[string]string{"type": "gp2", "fsType": "ext4"},
			wantLabel:       false,
		},
		{
			name:             "custom name creates a separate class",
			cfg:              &Config{EBS: &EBSConfig{Type: "gp2", StorageClassName: "gp2-encrypted"}},
			existing:         []runtime.Object{gp2()},
			wantClass:        "gp2-encrypted",
			wantDefault:      true,
			wantOtherDefault: map[string]bool{"gp2": false},
			wantProvisioner:  ebsCSIProvisioner,
			wantParams:       map[string]string{"type": "gp2", "encrypted": "true"},
			wantLabel:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewSimpleClientset(tt.existing...)
			if err := ensureEBSStorageClassWithClient(ctx, client, tt.cfg); err != nil {
				t.Fatalf("ensureEBSStorageClassWithClient() error = %v", err)
			}

			got, err := client.StorageV1().StorageClasses().Get(ctx, tt.wantClass, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("StorageClass %q not found: %v", tt.wantClass, err)
			}
			if got.Provisioner != tt.wantProvisioner {
				t.Errorf("Provisioner = %q, want %q", got.Provisioner, tt.wantProvisioner)
			}
			if !maps.Equal(got.Parameters, tt.wantParams) {
				t.Errorf("Parameters = %v, want %v", got.Parameters, tt.wantParams)
			}
			if hasLabel := got.Labels[managedByLabel] == managedByValue; hasLabel != tt.wantLabel {
				t.Errorf("managed-by label = %v, want %v", hasLabel, tt.wantLabel)
			}
			if isDefault := got.Annotations[storageclass.DefaultAnnotation] == "true"; isDefault != tt.wantDefault {
				t.Errorf("%s default = %v, want %v", tt.wantClass, isDefault, tt.wantDefault)
			}

			for name, wantDefault := range tt.wantOtherDefault {
				other, err := client.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
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
