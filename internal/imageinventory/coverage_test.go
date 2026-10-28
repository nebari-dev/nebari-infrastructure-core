package imageinventory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guard the places which can introduce deployment sources outside the generic
// Argo CD renderer. A changed installer/module needs an explicit coverage review,
// even if it adds a second chart to an already enrolled file. Updating this file
// is deliberately not part of inventory generation; see the operations guide.
var sourcePattern = regexp.MustCompile(`(?i)helm\.sh/helm|helm_release|//go:embed|\bimage\b|imageName|NodeImage|exec\.Command|module\s+"|DefaultHetznerK3sVersion`)

func deploymentSources(root string) (map[string]string, error) {
	got := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".tf") {
			return nil
		}
		body, err := os.ReadFile(name) //nolint:gosec // Walk is limited to the repository's pkg source tree.
		if err != nil {
			return err
		}
		if sourcePattern.Match(body) {
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			got[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(body))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Kind contains deployment defaults outside NIC's source tree.
	mod, err := os.ReadFile(filepath.Join(root, "go.mod")) //nolint:gosec // root is the test's fixed repository root.
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.Contains(line, "sigs.k8s.io/kind ") {
			got["go.mod:kind"] = strings.TrimSpace(line)
		}
	}
	return got, nil
}

func TestDeploymentSourceCoverage(t *testing.T) {
	var want map[string]string
	if err := json.Unmarshal(Read(t, "sources.json"), &want); err != nil {
		t.Fatal(err)
	}
	got, err := deploymentSources("../..")
	if err != nil {
		t.Fatal(err)
	}
	for name, digest := range got {
		if want[name] != digest {
			t.Errorf("review image inventory coverage for added/changed source %s, then update sources.json (new value %q)", name, digest)
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("remove stale source coverage entry %s", name)
		}
	}
}

func TestCoverageDiscoversSources(t *testing.T) {
	for _, source := range []string{`resource "helm_release" "new" {}`, `module "new" {}`, `image: example/new:1`, `pod := corev1.Container{Image: newImage}`, `action.NewInstall // helm.sh/helm/v3/pkg/action`, `//go:embed manifests`, `exec.CommandContext(ctx, "kubectl")`} {
		t.Run(source, func(t *testing.T) {
			if !sourcePattern.MatchString(source) {
				t.Fatal("unguarded source")
			}
		})
	}
}
