package hetzner

import (
	"regexp"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/imageinventory"
)

func TestDeployedImageInventory(t *testing.T) {
	inv := imageinventory.New(t)
	base := "https://raw.githubusercontent.com/vitobotta/hetzner-k3s/" + DefaultHetznerK3sVersion + "/src/configuration/models/addons_config/"
	for _, addon := range []string{"csi_driver", "cloud_controller_manager", "system_upgrade_controller"} {
		body := imageinventory.Fetch(t, base+addon+".cr")
		urls := regexp.MustCompile(`(?:getter|property) (?:deployment_)?manifest_url : String = "([^"]+)"`).FindAllSubmatch(body, -1)
		if len(urls) != 1 {
			t.Fatalf("review changed hetzner-k3s %s manifest source", addon)
		}
		url := string(urls[0][1])
		if err := inv.Manifest(imageinventory.Fetch(t, url), "pkg/providers/cluster/hetzner/cluster.go -> "+url); err != nil {
			t.Fatal(err)
		}
	}
	body := imageinventory.Fetch(t, base+"cluster_autoscaler.cr")
	tag := regexp.MustCompile(`property container_image_tag : String = "([^"]+)"`).FindSubmatch(body)
	if len(tag) != 2 {
		t.Fatal("review changed hetzner-k3s autoscaler image source")
	}
	if err := inv.Add("registry.k8s.io/autoscaling/cluster-autoscaler:"+string(tag[1]), "pkg/providers/cluster/hetzner/cluster.go -> hetzner-k3s "+DefaultHetznerK3sVersion); err != nil {
		t.Fatal(err)
	}
	inv.Notes = append(inv.Notes, "Hetzner add-ons are read from the pinned hetzner-k3s source and its upstream manifests, including optional autoscaling. k3s itself, metrics-server, CoreDNS, local-path-provisioner, embedded registry and upgrade images depend on the user-selected/resolved k3s release, which NIC does not pin. Traefik/servicelb are disabled; CNI is flannel, not the optional Cilium chart.")
	inv.Save(t, "hetzner")
}
