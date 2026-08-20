package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverProviderConfigFilesFindsRealProviders is a smoke test that the
// glob in discoverProviderConfigFiles still matches every provider package
// that exists today, with curated doc titles picked up from providerDocMeta.
func TestDiscoverProviderConfigFilesFindsRealProviders(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := findProjectRoot(wd)

	groups, err := discoverProviderGroups(root)
	if err != nil {
		t.Fatalf("discoverProviderGroups: %v", err)
	}

	discovered, err := discoverProviderConfigFiles(root, groups)
	if err != nil {
		t.Fatalf("discoverProviderConfigFiles: %v", err)
	}

	want := map[string]string{
		"pkg/providers/cluster/aws/config.go":      "AWS Provider Configuration",
		"pkg/providers/cluster/gcp/config.go":      "GCP Provider Configuration",
		"pkg/providers/cluster/azure/config.go":    "Azure Provider Configuration",
		"pkg/providers/cluster/hetzner/config.go":  "Hetzner Provider Configuration",
		"pkg/providers/cluster/local/config.go":    "Local Provider Configuration",
		"pkg/providers/cluster/existing/config.go": "Existing Cluster Configuration",
		"pkg/providers/dns/cloudflare/config.go":   "Cloudflare DNS Configuration",

		// The repository category was added to pkg/providers/ after docgen was
		// written and went undocumented until the category list stopped being
		// hand-maintained. Pin it so a third category can't regress the same way.
		"pkg/providers/repository/local/config.go":    "Local GitOps Repository Configuration",
		"pkg/providers/repository/existing/config.go": "Existing GitOps Repository Configuration",
	}

	got := make(map[string]string)
	for _, cf := range discovered {
		got[cf.path] = cf.docTitle
	}

	for path, title := range want {
		gotTitle, ok := got[path]
		if !ok {
			t.Errorf("discoverProviderConfigFiles did not find %s", path)
			continue
		}
		if gotTitle != title {
			t.Errorf("docTitle for %s = %q, want %q", path, gotTitle, title)
		}
	}
}

// TestDiscoverProviderConfigFilesPicksUpNewProvider proves the fix for the
// original bug: a brand new provider directory, never mentioned anywhere in
// docgen, is discovered automatically with a generated title/description
// rather than being silently skipped.
func TestDiscoverProviderConfigFilesPicksUpNewProvider(t *testing.T) {
	root := t.TempDir()
	providerDir := filepath.Join(root, "pkg", "providers", "cluster", "newcloud")
	if err := os.MkdirAll(providerDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	src := "package newcloud\n\ntype Config struct {\n\tRegion string `yaml:\"region\"`\n}\n"
	if err := os.WriteFile(filepath.Join(providerDir, "config.go"), []byte(src), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	discovered, err := discoverProviderConfigFiles(root, []string{"cluster"})
	if err != nil {
		t.Fatalf("discoverProviderConfigFiles: %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("got %d discovered config files, want 1: %+v", len(discovered), discovered)
	}
	cf := discovered[0]
	if cf.path != "pkg/providers/cluster/newcloud/config.go" {
		t.Errorf("path = %q, want %q", cf.path, "pkg/providers/cluster/newcloud/config.go")
	}
	if cf.docTitle != "Newcloud Provider Configuration" {
		t.Errorf("docTitle = %q, want a generated fallback title", cf.docTitle)
	}
	if len(cf.structs) != 0 {
		t.Errorf("structs = %v, want empty (discovered files document every exported struct)", cf.structs)
	}
}

// TestProcessConfigFileFailsOnNoExportedStructs proves the second half of
// the fix: a provider directory whose config.go happens to have zero
// exported structs is a hard failure, not a silently empty page.
func TestProcessConfigFileFailsOnNoExportedStructs(t *testing.T) {
	root := t.TempDir()
	providerDir := filepath.Join(root, "pkg", "providers", "cluster", "empty")
	if err := os.MkdirAll(providerDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	src := "package empty\n\ntype config struct {\n\tRegion string `yaml:\"region\"`\n}\n"
	if err := os.WriteFile(filepath.Join(providerDir, "config.go"), []byte(src), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	outPath := filepath.Join(root, "out")
	if err := os.MkdirAll(outPath, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cf := configFile{path: "pkg/providers/cluster/empty/config.go", docTitle: "Empty", docDesc: "Empty."}
	if _, err := processConfigFile(root, outPath, cf, false); err == nil {
		t.Error("processConfigFile: want error for a config.go with no exported structs, got nil")
	}
}

// TestDiscoverProviderGroupsFindsNewCategory covers the gap that let
// pkg/providers/repository/ ship undocumented: a category nobody enumerated
// produced no pages, so there was no diff for the docs gate to fail on.
func TestDiscoverProviderGroupsFindsNewCategory(t *testing.T) {
	root := t.TempDir()

	for _, dir := range []string{"cluster/aws", "dns/cloudflare", "brandnew/thing"} {
		full := filepath.Join(root, "pkg", "providers", filepath.FromSlash(dir))
		if err := os.MkdirAll(full, 0750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(full, "config.go"), []byte("package p\n"), 0600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	// A category directory with no provider holding a config.go isn't a category.
	if err := os.MkdirAll(filepath.Join(root, "pkg", "providers", "empty"), 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	groups, err := discoverProviderGroups(root)
	if err != nil {
		t.Fatalf("discoverProviderGroups: %v", err)
	}

	want := []string{"brandnew", "cluster", "dns"}
	if len(groups) != len(want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}
	for i, g := range want {
		if groups[i] != g {
			t.Errorf("groups[%d] = %q, want %q (sorted for deterministic output)", i, groups[i], g)
		}
	}
}

// Two providers with the same name in different categories must not both claim
// the same page - the second write would silently overwrite the first while the
// index still advertised both.
func TestGenerateOutputNameQualifiesNonBareGroups(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"pkg/providers/cluster/local/config.go", "local.md"},
		{"pkg/providers/cluster/aws/config.go", "aws.md"},
		{"pkg/providers/dns/cloudflare/config.go", "cloudflare.md"},
		{"pkg/providers/repository/local/config.go", "repository-local.md"},
		{"pkg/providers/repository/existing/config.go", "repository-existing.md"},
		{"pkg/config/config.go", "core.md"},
		{"pkg/config/trust_bundle.go", "trust-bundle.md"},
	}

	for _, tt := range tests {
		if got := generateOutputName(tt.path); got != tt.want {
			t.Errorf("generateOutputName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestCheckOutputNameCollisions(t *testing.T) {
	ok := []configFile{
		{path: "pkg/providers/cluster/local/config.go"},
		{path: "pkg/providers/repository/local/config.go"},
		{path: "pkg/config/config.go"},
	}
	if err := checkOutputNameCollisions(ok); err != nil {
		t.Errorf("checkOutputNameCollisions on distinct pages: %v", err)
	}

	// Both land on local.md once the repository group is treated as bare.
	clashing := []configFile{
		{path: "pkg/providers/cluster/local/config.go"},
		{path: "pkg/providers/dns/local/config.go"},
	}
	err := checkOutputNameCollisions(clashing)
	if err == nil {
		t.Fatal("checkOutputNameCollisions: want an error for two sources claiming local.md, got nil")
	}
	if !strings.Contains(err.Error(), "local.md") {
		t.Errorf("error should name the colliding page, got: %v", err)
	}
}
