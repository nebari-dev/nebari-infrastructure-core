package azure

import (
	"strings"
	"testing"
)

func TestBuildTagFilter(t *testing.T) {
	got := buildTagFilter("my-cluster")
	if !strings.Contains(got, "nic.nebari.dev_cluster-name") {
		t.Errorf("filter missing cluster-name tag: %s", got)
	}
	if !strings.Contains(got, "my-cluster") {
		t.Errorf("filter missing project name: %s", got)
	}
}
