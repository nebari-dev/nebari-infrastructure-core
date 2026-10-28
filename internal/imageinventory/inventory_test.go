package imageinventory

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestManifestImages(t *testing.T) {
	for _, tt := range []struct {
		name, manifest string
		images         []string
		wantErr        bool
	}{
		{"containers and hooks", "kind: Job\nspec:\n  template:\n    spec:\n      initContainers: [{image: busybox:1.37}]\n      containers: [{image: quay.io/keycloak/keycloak:26.7.2}]", []string{"busybox:1.37", "quay.io/keycloak/keycloak:26.7.2"}, false},
		{"database CR", "kind: Cluster\nspec: {imageName: ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie}", []string{"ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie"}, false},
		{"operator flags", "kind: Deployment\nspec: {args: ['--engine-image=docker.io/longhornio/longhorn-engine:v1.11.2']}", []string{"docker.io/longhornio/longhorn-engine:v1.11.2"}, false},
		{"unresolved operator environment fails", "kind: Deployment\nspec: {env: [{name: POSTGRES_IMAGE_NAME, valueFrom: {secretKeyRef: {name: postgres, key: image}}}]}", nil, true},
		{"GPU components", "kind: ClusterPolicy\nspec:\n  driver: {enabled: false, repository: nvcr.io/nvidia, image: driver, version: '580'}\n  devicePlugin: {enabled: true, repository: nvcr.io/nvidia, image: k8s-device-plugin, version: v0.19.2}", []string{"nvcr.io/nvidia/k8s-device-plugin:v0.19.2"}, false},
		{"CRD examples excluded", "kind: CustomResourceDefinition\nspec: {image: placeholder}", nil, false},
		{"unresolved image fails", "kind: Pod\nspec: {containers: [{image: controller}]}", nil, true},
		{"invalid YAML fails", "kind: [", nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inv := &Inventory{Images: map[string][]string{}}
			err := inv.Manifest([]byte(tt.manifest), "source.yaml")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error: %v", err)
			}
			var got []string
			for image := range inv.Images {
				got = append(got, image)
			}
			slices.Sort(got)
			if !reflect.DeepEqual(got, tt.images) {
				t.Fatalf("got %v, want %v", got, tt.images)
			}
		})
	}
}

func TestChartDefaultAndOverride(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values map[string]any
		want   string
	}{
		{"chart appVersion", map[string]any{}, "quay.io/keycloak/keycloak:26.5.0"},
		{"NIC values override", map[string]any{"image": map[string]any{"tag": "26.7.2"}}, "quay.io/keycloak/keycloak:26.7.2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "templates"), 0o750); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"Chart.yaml":         "apiVersion: v2\nname: keycloakx\nversion: 7.1.6\nappVersion: 26.5.0\n",
				"values.yaml":        "image:\n  repository: quay.io/keycloak/keycloak\n  tag: ''\n",
				"templates/pod.yaml": "kind: Pod\nspec:\n  containers:\n    - image: {{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			inv := &Inventory{Images: map[string][]string{}}
			inv.LocalChart(t, "values/keycloak/base.yaml", dir, "keycloak", "keycloak", tt.values)
			if len(inv.Images) != 1 || inv.Images[tt.want] == nil {
				t.Fatalf("unexpected images: %v", inv.Images)
			}
		})
	}
}

func TestPostgresDefaultImages(t *testing.T) {
	for _, tt := range []struct {
		name, defaultImage, manifest, want string
		wantErr                            bool
	}{
		{"operator default", "registry.example/postgres:18", "apiVersion: postgresql.cnpg.io/v1\nkind: Cluster\nspec: {}", "registry.example/postgres:18", false},
		{"Cluster pin wins", "registry.example/postgres:18", "apiVersion: postgresql.cnpg.io/v1\nkind: Cluster\nspec: {imageName: registry.example/postgres:17}", "registry.example/postgres:17", false},
		{"missing operator default fails", "", "apiVersion: postgresql.cnpg.io/v1\nkind: Cluster\nspec: {}", "", true},
		{"unhandled image catalog fails", "registry.example/postgres:18", "apiVersion: postgresql.cnpg.io/v1\nkind: Cluster\nspec: {imageCatalogRef: {name: other}}", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inv := &Inventory{Images: map[string][]string{}, postgresDefault: tt.defaultImage}
			err := inv.Manifest([]byte(tt.manifest), "cluster.yaml")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error: %v", err)
			}
			if !tt.wantErr && (len(inv.Images) != 1 || inv.Images[tt.want] == nil) {
				t.Fatalf("unexpected images: %v", inv.Images)
			}
		})
	}
}
