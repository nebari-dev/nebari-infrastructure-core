package main

import (
	"os"
	"path/filepath"
	"testing"
)

const testFixture = `package testpkg

// Config is the root config struct.
type Config struct {
	// Region is the cloud provider region.
	Region string ` + "`" + `yaml:"region"` + "`" + `
	// Tags are optional metadata labels.
	Tags map[string]string ` + "`" + `yaml:"tags,omitempty"` + "`" + `
	// Count is an optional integer.
	Count int ` + "`" + `yaml:"count,omitempty"` + "`" + `
	// Sub is a nested struct.
	Sub *SubConfig ` + "`" + `yaml:"sub,omitempty"` + "`" + `
	// skipped is unexported.
	skipped string
}

// SubConfig is a nested struct.
type SubConfig struct {
	Name string ` + "`" + `yaml:"name"` + "`" + `
}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.go")
	if err := os.WriteFile(path, []byte(testFixture), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFile_Fields(t *testing.T) {
	path := writeFixture(t)

	structs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg, ok := structs["Config"]
	if !ok {
		t.Fatal("Config struct not found")
	}
	if len(cfg.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d: %+v", len(cfg.Fields), cfg.Fields)
	}

	region := cfg.Fields[0]
	if region.YAMLKey != "region" {
		t.Errorf("field[0] YAMLKey: want %q, got %q", "region", region.YAMLKey)
	}
	if !region.Required {
		t.Error("region should be required (no omitempty)")
	}
	if region.GoType != "string" {
		t.Errorf("region GoType: want %q, got %q", "string", region.GoType)
	}
	if region.Doc == "" {
		t.Error("region should have a doc comment")
	}

	tags := cfg.Fields[1]
	if tags.YAMLKey != "tags" {
		t.Errorf("field[1] YAMLKey: want %q, got %q", "tags", tags.YAMLKey)
	}
	if tags.Required {
		t.Error("tags should be optional (has omitempty)")
	}
	if tags.GoType != "map[string]string" {
		t.Errorf("tags GoType: want %q, got %q", "map[string]string", tags.GoType)
	}

	sub := cfg.Fields[3]
	if sub.YAMLKey != "sub" {
		t.Errorf("field[3] YAMLKey: want %q, got %q", "sub", sub.YAMLKey)
	}
	if sub.GoType != "SubConfig (optional)" {
		t.Errorf("sub GoType: want %q, got %q", "SubConfig (optional)", sub.GoType)
	}
}

func TestParseFile_SubStruct(t *testing.T) {
	path := writeFixture(t)

	structs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	sub, ok := structs["SubConfig"]
	if !ok {
		t.Fatal("SubConfig not found in parsed structs")
	}
	if len(sub.Fields) != 1 || sub.Fields[0].YAMLKey != "name" {
		t.Errorf("unexpected SubConfig fields: %+v", sub.Fields)
	}
}

func TestParseFile_SkipsUnexported(t *testing.T) {
	path := writeFixture(t)

	structs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := structs["Config"]
	for _, f := range cfg.Fields {
		if f.YAMLKey == "" && !f.Inline {
			t.Errorf("unexpected blank-key non-inline field leaked through: %+v", f)
		}
	}
}

func TestParseFile_StructDoc(t *testing.T) {
	path := writeFixture(t)

	structs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	cfg := structs["Config"]
	if cfg.Doc == "" {
		t.Error("Config struct should have a doc comment")
	}
}
