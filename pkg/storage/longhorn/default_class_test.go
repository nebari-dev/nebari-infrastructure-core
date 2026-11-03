package longhorn

import (
	"context"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

// A deploy with default_class: false must leave the admin's default class
// alone. Without the gate in claimDefaultStorageClass it would take the
// default away from gp3 on every install and upgrade.
func TestClaimDefaultStorageClass(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *Config
		wantGP3Default bool
	}{
		{name: "nil config claims the default", cfg: nil, wantGP3Default: false},
		{name: "default_class unset claims the default", cfg: &Config{}, wantGP3Default: false},
		{name: "default_class true claims the default", cfg: &Config{DefaultClass: boolPtr(true)}, wantGP3Default: false},
		{name: "default_class false keeps the gp3 default", cfg: &Config{DefaultClass: boolPtr(false)}, wantGP3Default: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(
				&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{
					Name:        "gp3",
					Annotations: map[string]string{storageclass.DefaultAnnotation: "true"},
				}},
				&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: StorageClassName}},
			)

			if err := claimDefaultStorageClass(context.Background(), client, tt.cfg); err != nil {
				t.Fatalf("claimDefaultStorageClass() error = %v", err)
			}

			gp3, err := client.StorageV1().StorageClasses().Get(context.Background(), "gp3", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get gp3: %v", err)
			}
			if got := storageclass.IsDefault(gp3); got != tt.wantGP3Default {
				t.Errorf("gp3 is default = %v, want %v", got, tt.wantGP3Default)
			}
		})
	}
}
