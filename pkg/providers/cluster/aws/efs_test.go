package aws

import (
	"context"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEFSStorageClassName(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   string
	}{
		{
			name:   "nil EFS config returns default",
			config: &Config{},
			want:   defaultEFSStorageClassName,
		},
		{
			name: "empty StorageClassName returns default",
			config: &Config{
				EFS: &EFSConfig{Enabled: true},
			},
			want: defaultEFSStorageClassName,
		},
		{
			name: "custom StorageClassName is returned",
			config: &Config{
				EFS: &EFSConfig{
					Enabled:          true,
					StorageClassName: "my-efs",
				},
			},
			want: "my-efs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.EFSStorageClassName()
			if got != tt.want {
				t.Errorf("EFSStorageClassName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateEFSStorageClassWithClient(t *testing.T) {
	tests := []struct {
		name             string
		config           *Config
		efsID            string
		existing         []runtime.Object
		wantErr          bool
		wantSCName       string
		wantProvisioner  string
		wantFileSystemID string
	}{
		{
			name: "creates StorageClass when it does not exist",
			config: &Config{
				EFS: &EFSConfig{Enabled: true},
			},
			efsID:            "fs-12345678",
			existing:         nil,
			wantErr:          false,
			wantSCName:       defaultEFSStorageClassName,
			wantProvisioner:  efsCSIProvisioner,
			wantFileSystemID: "fs-12345678",
		},
		{
			name: "creates StorageClass with custom name",
			config: &Config{
				EFS: &EFSConfig{
					Enabled:          true,
					StorageClassName: "custom-efs",
				},
			},
			efsID:            "fs-abcdef01",
			existing:         nil,
			wantErr:          false,
			wantSCName:       "custom-efs",
			wantProvisioner:  efsCSIProvisioner,
			wantFileSystemID: "fs-abcdef01",
		},
		{
			name: "rejects empty efsID",
			config: &Config{
				EFS: &EFSConfig{Enabled: true},
			},
			efsID:   "",
			wantErr: true,
		},
		{
			name: "updates StorageClass when it already exists",
			config: &Config{
				EFS: &EFSConfig{Enabled: true},
			},
			efsID: "fs-new-id",
			existing: []runtime.Object{
				&storagev1.StorageClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: defaultEFSStorageClassName,
					},
					Provisioner: efsCSIProvisioner,
					Parameters: map[string]string{
						"fileSystemId": "fs-old-id",
					},
				},
			},
			wantErr:          false,
			wantSCName:       defaultEFSStorageClassName,
			wantProvisioner:  efsCSIProvisioner,
			wantFileSystemID: "fs-new-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...) //nolint:staticcheck

			err := createEFSStorageClassWithClient(context.Background(), client, tt.config, tt.efsID)
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

			if sc.Provisioner != tt.wantProvisioner {
				t.Errorf("Provisioner = %q, want %q", sc.Provisioner, tt.wantProvisioner)
			}

			if sc.Parameters["fileSystemId"] != tt.wantFileSystemID {
				t.Errorf("fileSystemId = %q, want %q", sc.Parameters["fileSystemId"], tt.wantFileSystemID)
			}

			if sc.Parameters["provisioningMode"] != "efs-ap" {
				t.Errorf("provisioningMode = %q, want %q", sc.Parameters["provisioningMode"], "efs-ap")
			}

			if sc.Parameters["directoryPerms"] != "700" {
				t.Errorf("directoryPerms = %q, want %q", sc.Parameters["directoryPerms"], "700")
			}

			if sc.ReclaimPolicy == nil || string(*sc.ReclaimPolicy) != "Retain" {
				t.Errorf("ReclaimPolicy = %v, want Retain", sc.ReclaimPolicy)
			}

			if sc.VolumeBindingMode == nil || string(*sc.VolumeBindingMode) != "Immediate" {
				t.Errorf("VolumeBindingMode = %v, want Immediate", sc.VolumeBindingMode)
			}
		})
	}
}
