package longhorn

import (
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/imageinventory"
)

func TestDeployedImageInventory(t *testing.T) {
	inv := imageinventory.New(t)
	inv.Chart(t, "pkg/storage/longhorn/install.go", chartRepoURL, strings.TrimPrefix(chartName, chartRepoName+"/"), ChartVersion, ReleaseName, Namespace, buildHelmValues(&Config{DedicatedNodes: true}))
	if err := inv.Manifest([]byte(iscsiDaemonSetYAML), "pkg/storage/longhorn/iscsi.go"); err != nil {
		t.Fatal(err)
	}
	inv.Save(t, "longhorn")
}
