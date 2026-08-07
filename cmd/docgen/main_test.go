package main

import (
	"os"
	"path/filepath"
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

	discovered, err := discoverProviderConfigFiles(root)
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

	discovered, err := discoverProviderConfigFiles(root)
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
	if err := processConfigFile(root, outPath, cf, false); err == nil {
		t.Error("processConfigFile: want error for a config.go with no exported structs, got nil")
	}
}
