package imageinventory

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// KindNode includes the images preloaded inside Kind's pinned node container.
// Scanning just the outer filesystem misses the containerd image layers. crane
// exports data without starting any container or executing code from the image.
func (i *Inventory) KindNode(t *testing.T, image, platform string) {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "node.tar")
	cmd := exec.CommandContext(t.Context(), "crane", "export", "--platform", platform, image, archive) //nolint:gosec // Image is Kind's compiled default; no shell, export only.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("export Kind node (requires crane): %v: %s", err, out)
	}
	f, err := os.Open(archive) //nolint:gosec // Fixed filename inside t.TempDir, created by the export above.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r := tar.NewReader(f)
	for {
		header, err := r.Next()
		if err == io.EOF {
			t.Fatal("Kind node no longer contains expected containerd metadata; review inventory coverage")
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != "var/lib/containerd/io.containerd.metadata.v1.bolt/meta.db" {
			continue
		}
		if header.Size > 32<<20 {
			t.Fatal("unexpected Kind metadata size")
		}
		data, err := io.ReadAll(io.LimitReader(r, 32<<20))
		if err != nil {
			t.Fatal(err)
		}
		metadata := filepath.Join(dir, "meta.db")
		if err := os.WriteFile(metadata, data, 0o600); err != nil {
			t.Fatal(err)
		}
		images, err := nodeImages(metadata)
		if err != nil {
			t.Fatal(err)
		}
		for _, image := range images {
			if err := i.Add(image, "pkg/providers/cluster/local/kind.go -> Kind node containerd inventory "+platform+" (go.mod)"); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
}

func nodeImages(filename string) ([]string, error) {
	db, err := bolt.Open(filename, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	var images []string
	err = db.View(func(tx *bolt.Tx) error {
		v1 := tx.Bucket([]byte("v1"))
		if v1 == nil {
			return fmt.Errorf("unknown containerd metadata version")
		}
		ns := v1.Bucket([]byte("k8s.io"))
		if ns == nil {
			return fmt.Errorf("missing containerd Kubernetes namespace")
		}
		bucket := ns.Bucket([]byte("images"))
		if bucket == nil {
			return fmt.Errorf("missing containerd images")
		}
		return bucket.ForEach(func(key, _ []byte) error {
			name := string(key)
			// containerd stores config-digest and import aliases in addition
			// to registry names. They are not separately pullable images.
			if strings.HasPrefix(name, "sha256:") || strings.HasPrefix(name, "import-") {
				return nil
			}
			// kubeadm's architecture-suffixed aliases contain the same image.
			for _, arch := range []string{"amd64", "arm64"} {
				if strings.Contains(name, "-"+arch+":") && bucket.Bucket([]byte(strings.Replace(name, "-"+arch+":", ":", 1))) != nil {
					return nil
				}
			}
			images = append(images, name)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("empty Kind image cache")
	}
	slices.Sort(images)
	return images, nil
}
