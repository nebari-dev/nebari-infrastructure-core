package imageinventory

import (
	"path/filepath"
	"reflect"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestNodeImages(t *testing.T) {
	for _, tt := range []struct {
		name    string
		entries []string
		want    []string
		wantErr bool
	}{
		{"registry names and aliases", []string{"registry.k8s.io/kube-proxy:v1.36.1", "registry.k8s.io/kube-proxy-amd64:v1.36.1", "sha256:1234", "import-cache@sha256:1234", "docker.io/kindest/kindnetd:v1"}, []string{"docker.io/kindest/kindnetd:v1", "registry.k8s.io/kube-proxy:v1.36.1"}, false},
		{"empty fails", nil, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "meta.db")
			db, err := bolt.Open(filename, 0o600, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = db.Update(func(tx *bolt.Tx) error {
				bucket, err := tx.CreateBucket([]byte("v1"))
				if err != nil {
					return err
				}
				for _, name := range []string{"k8s.io", "images"} {
					bucket, err = bucket.CreateBucket([]byte(name))
					if err != nil {
						return err
					}
				}
				for _, name := range tt.entries {
					if _, err := bucket.CreateBucket([]byte(name)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := nodeImages(filename)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
