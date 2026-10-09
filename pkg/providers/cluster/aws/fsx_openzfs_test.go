package aws

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateFSxOpenZFSStorageClassWithClient(t *testing.T) {
	tests := []struct {
		name               string
		config             *Config
		rootVolumeID       string
		existing           []runtime.Object
		wantErr            bool
		wantSCName         string
		wantParentVolumeID string
	}{
		{
			name:               "creates StorageClass when it does not exist",
			config:             &Config{FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true}},
			rootVolumeID:       "fsvol-0123456789abcdef0",
			wantSCName:         defaultFSxOpenZFSStorageClassName,
			wantParentVolumeID: `"fsvol-0123456789abcdef0"`,
		},
		{
			name:               "creates StorageClass with custom name",
			config:             &Config{FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true, StorageClassName: "shared"}},
			rootVolumeID:       "fsvol-abc",
			wantSCName:         "shared",
			wantParentVolumeID: `"fsvol-abc"`,
		},
		{
			name:         "rejects empty root volume ID",
			config:       &Config{FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true}},
			rootVolumeID: "",
			wantErr:      true,
		},
		{
			name:         "updates StorageClass when it already exists",
			config:       &Config{FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true}},
			rootVolumeID: "fsvol-new",
			existing: []runtime.Object{
				&storagev1.StorageClass{
					ObjectMeta:  metav1.ObjectMeta{Name: defaultFSxOpenZFSStorageClassName},
					Provisioner: fsxOpenZFSCSIProvisioner,
					Parameters:  map[string]string{"ParentVolumeId": `"fsvol-old"`},
				},
			},
			wantSCName:         defaultFSxOpenZFSStorageClassName,
			wantParentVolumeID: `"fsvol-new"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...)

			err := createFSxOpenZFSStorageClassWithClient(context.Background(), client, tt.config, tt.rootVolumeID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			sc, err := client.StorageV1().StorageClasses().Get(context.Background(), tt.wantSCName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("StorageClass %q not found: %v", tt.wantSCName, err)
			}

			if sc.Provisioner != fsxOpenZFSCSIProvisioner {
				t.Errorf("Provisioner = %q, want %q", sc.Provisioner, fsxOpenZFSCSIProvisioner)
			}

			wantParams := map[string]string{
				"ResourceType":        "volume",
				"ParentVolumeId":      tt.wantParentVolumeID,
				"DataCompressionType": `"LZ4"`,
				"NfsExports":          fsxOpenZFSNFSExports,
				"OptionsOnDeletion":   `["DELETE_CHILD_VOLUMES_AND_SNAPSHOTS"]`,
			}
			if !reflect.DeepEqual(sc.Parameters, wantParams) {
				t.Errorf("Parameters = %v, want %v", sc.Parameters, wantParams)
			}

			if sc.ReclaimPolicy == nil || string(*sc.ReclaimPolicy) != "Delete" {
				t.Errorf("ReclaimPolicy = %v, want Delete", sc.ReclaimPolicy)
			}
			if sc.VolumeBindingMode == nil || string(*sc.VolumeBindingMode) != "Immediate" {
				t.Errorf("VolumeBindingMode = %v, want Immediate", sc.VolumeBindingMode)
			}
			if sc.AllowVolumeExpansion == nil || *sc.AllowVolumeExpansion {
				t.Errorf("AllowVolumeExpansion = %v, want false", sc.AllowVolumeExpansion)
			}
			wantMount := []string{"nfsvers=4.1", "rsize=1048576", "wsize=1048576", "timeo=600"}
			if !reflect.DeepEqual(sc.MountOptions, wantMount) {
				t.Errorf("MountOptions = %v, want %v", sc.MountOptions, wantMount)
			}
		})
	}
}

// TestFSxOpenZFSStorageClassParametersAreJSON guards the driver's contract
// that every parameter except ResourceType is valid JSON. A typo here only
// surfaces as a failed PVC on a live cluster.
func TestFSxOpenZFSStorageClassParametersAreJSON(t *testing.T) {
	params := map[string]string{
		"DataCompressionType": fsxOpenZFSDataCompression,
		"NfsExports":          fsxOpenZFSNFSExports,
		"OptionsOnDeletion":   fsxOpenZFSOptionsOnDeletion,
	}
	for name, value := range params {
		if !json.Valid([]byte(value)) {
			t.Errorf("%s = %s is not valid JSON", name, value)
		}
	}

	var exports []struct {
		ClientConfigurations []struct {
			Clients string
			Options []string
		}
	}
	if err := json.Unmarshal([]byte(fsxOpenZFSNFSExports), &exports); err != nil {
		t.Fatalf("NfsExports does not decode: %v", err)
	}
	opts := exports[0].ClientConfigurations[0].Options
	found := false
	for _, o := range opts {
		if o == "no_root_squash" {
			found = true
		}
	}
	if !found {
		t.Errorf("NfsExports options %v must include no_root_squash so kubelet can apply fsGroup", opts)
	}
}
