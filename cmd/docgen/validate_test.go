package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildConfigFixture writes a minimal "config" package and returns its base dir
// (parent of the package dir) plus a parser keyed by struct name.
func buildConfigFixture(t *testing.T) (base string, parse func(name string) StructDoc) {
	t.Helper()
	base = t.TempDir()
	dir := filepath.Join(base, "config")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	src := `package config
type NebariConfig struct {
	Backups *BackupsConfig
	Cluster *ClusterConfig
	Cert    *CertConfig
	Name    string
}
type BackupsConfig struct{ Bucket string }
type ClusterConfig struct{ Provider string }
type CertConfig struct{ Type string }
`
	file := filepath.Join(dir, "config.go")
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	structs, err := ParseFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return base, func(name string) StructDoc {
		for _, s := range structs {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("struct %q not found in fixture", name)
		return StructDoc{}
	}
}

func TestValidateDocumentedRefs_FlagsUndocumentedReference(t *testing.T) {
	base, parse := buildConfigFixture(t)
	root, cert := parse("NebariConfig"), parse("CertConfig")

	// NebariConfig references BackupsConfig (defined, not documented),
	// ClusterConfig (deliberately excluded), and CertConfig (documented).
	// Only BackupsConfig should be reported.
	err := validateDocumentedRefs([]StructDoc{root, cert}, base)
	if err == nil {
		t.Fatal("expected a gap error for undocumented BackupsConfig, got nil")
	}
	if !strings.Contains(err.Error(), "BackupsConfig") {
		t.Fatalf("error should name BackupsConfig, got: %v", err)
	}
	if strings.Contains(err.Error(), "ClusterConfig") {
		t.Fatalf("ClusterConfig is deliberately undocumented and must not be flagged: %v", err)
	}
	if strings.Contains(err.Error(), "CertConfig") {
		t.Fatalf("CertConfig is documented and must not be flagged: %v", err)
	}
}

func TestValidateDocumentedRefs_PassesWhenAllDocumented(t *testing.T) {
	base, parse := buildConfigFixture(t)
	rendered := []StructDoc{parse("NebariConfig"), parse("CertConfig"), parse("BackupsConfig")}
	if err := validateDocumentedRefs(rendered, base); err != nil {
		t.Fatalf("expected no gap once every referenced struct has a page, got: %v", err)
	}
}

func TestRefsInGoType(t *testing.T) {
	cases := map[string][]typeRef{
		"*BackupsConfig":        {{name: "BackupsConfig"}},
		"[]Taint":               {{name: "Taint"}},
		"map[string]*NodeGroup": {{name: "NodeGroup"}},
		"*longhorn.Config":      {{pkg: "longhorn", name: "Config"}},
		"string":                nil,
		"map[string]string":     nil,
	}
	for in, want := range cases {
		got := refsInGoType(in)
		if len(got) != len(want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: got %v, want %v", in, got, want)
			}
		}
	}
}
