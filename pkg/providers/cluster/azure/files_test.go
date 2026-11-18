package azure

import (
	"context"
	"strings"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const testSubnetID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/proj-rg/providers/Microsoft.Network/virtualNetworks/proj-vnet/subnets/proj-nodes"

func TestParseSubnetID(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		want    subnetRef
		wantErr bool
	}{
		{
			name: "subnet ID",
			id:   testSubnetID,
			want: subnetRef{ResourceGroup: "proj-rg", VNet: "proj-vnet", Subnet: "proj-nodes"},
		},
		{
			name:    "VNet ID",
			id:      "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/proj-rg/providers/Microsoft.Network/virtualNetworks/proj-vnet",
			wantErr: true,
		},
		{name: "empty", id: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSubnetID(tc.id)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseSubnetID() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFilesStorageClass(t *testing.T) {
	cfg := &Config{Files: &FilesConfig{Enabled: true, SKU: "Premium_ZRS"}}
	sc := filesStorageClass(cfg, subnetRef{ResourceGroup: "rg", VNet: "vn", Subnet: "sn"})

	if sc.Name != "azurefile-nfs" {
		t.Errorf("Name = %q, want azurefile-nfs", sc.Name)
	}
	if sc.Provisioner != "file.csi.azure.com" {
		t.Errorf("Provisioner = %q, want file.csi.azure.com", sc.Provisioner)
	}
	wantParams := map[string]string{
		"protocol":          "nfs",
		"skuName":           "Premium_ZRS",
		"rootSquashType":    "NoRootSquash",
		"vnetResourceGroup": "rg",
		"vnetName":          "vn",
		"subnetName":        "sn",
	}
	for k, v := range wantParams {
		if sc.Parameters[k] != v {
			t.Errorf("Parameters[%q] = %q, want %q", k, sc.Parameters[k], v)
		}
	}
	if len(sc.Parameters) != len(wantParams) {
		t.Errorf("Parameters = %v, want exactly %v", sc.Parameters, wantParams)
	}
}

func TestEnsureFilesStorageClassWithClient(t *testing.T) {
	cfg := &Config{Files: &FilesConfig{Enabled: true}}
	subnet, err := parseSubnetID(testSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	same := filesStorageClass(cfg, subnet)

	different := filesStorageClass(cfg, subnet)
	different.Parameters["skuName"] = "Premium_LRS"

	cases := []struct {
		name      string
		existing  []runtime.Object
		subnetID  string
		wantErr   string
		wantClass bool
	}{
		{name: "creates the class", subnetID: testSubnetID, wantClass: true},
		{name: "keeps an identical class", existing: []runtime.Object{same}, subnetID: testSubnetID, wantClass: true},
		{name: "refuses a class with other settings", existing: []runtime.Object{different}, subnetID: testSubnetID, wantErr: "kubectl delete storageclass azurefile-nfs"},
		{name: "rejects a bad subnet ID", subnetID: "not-an-id", wantErr: "subnet ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(tc.existing...)
			err := ensureFilesStorageClassWithClient(context.Background(), client, cfg, tc.subnetID)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := client.StorageV1().StorageClasses().Get(context.Background(), "azurefile-nfs", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("StorageClass not found: %v", err)
			}
			if !sameStorageClassSpec(got, same) {
				t.Errorf("StorageClass = %+v, want %+v", got, same)
			}
		})
	}
}

func TestSameStorageClassSpec(t *testing.T) {
	base := filesStorageClass(&Config{}, subnetRef{ResourceGroup: "rg", VNet: "vn", Subnet: "sn"})

	cases := []struct {
		name   string
		mutate func(*storagev1.StorageClass)
		want   bool
	}{
		{name: "identical", mutate: func(*storagev1.StorageClass) {}, want: true},
		{name: "labels are ignored", mutate: func(sc *storagev1.StorageClass) { sc.Labels = map[string]string{"a": "b"} }, want: true},
		{name: "provisioner", mutate: func(sc *storagev1.StorageClass) { sc.Provisioner = "other" }, want: false},
		{name: "parameter", mutate: func(sc *storagev1.StorageClass) { sc.Parameters["subnetName"] = "other" }, want: false},
		{name: "mount options", mutate: func(sc *storagev1.StorageClass) { sc.MountOptions = nil }, want: false},
		{name: "expansion", mutate: func(sc *storagev1.StorageClass) { sc.AllowVolumeExpansion = nil }, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := base.DeepCopy()
			tc.mutate(other)
			if got := sameStorageClassSpec(base, other); got != tc.want {
				t.Errorf("sameStorageClassSpec() = %v, want %v", got, tc.want)
			}
		})
	}
}
