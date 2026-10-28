package local

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/imageinventory"
	"sigs.k8s.io/kind/pkg/apis/config/defaults"
)

func TestDeployedImageInventory(t *testing.T) {
	inv := imageinventory.New(t)
	if err := inv.Add(defaults.Image, "pkg/providers/cluster/local/kind.go -> kind defaults.Image (go.mod)"); err != nil {
		t.Fatal(err)
	}
	loadBalancer := imageinventory.ModuleFile(t, "sigs.k8s.io/kind", "pkg/cluster/internal/loadbalancer/const.go")
	if err := inv.Add(imageinventory.GoString(t, loadBalancer, "Image"), "pkg/providers/cluster/local/kind.go -> kind HA load balancer (go.mod)"); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		inv.KindNode(t, defaults.Image, platform)
	}
	inv.Notes = append(inv.Notes, "Local: scans the bundled Kind node image, its preloaded Kubernetes/CNI/storage images and optional HA load balancer. User-specified node images are outside this default inventory. Existing provider installs no cluster software; GCP is a stub.")
	inv.Save(t, "local")
}
