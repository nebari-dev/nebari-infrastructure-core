package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		name     string
		tagValue string
		want     FieldDoc
	}{
		{
			name:     "simple yaml tag",
			tagValue: "`yaml:\"region\"`",
			want: FieldDoc{
				YAMLKey:  "region",
				Required: true,
			},
		},
		{
			name:     "yaml tag with omitempty",
			tagValue: "`yaml:\"domain,omitempty\"`",
			want: FieldDoc{
				YAMLKey:  "domain",
				Required: false,
			},
		},
		{
			name:     "yaml ignored field",
			tagValue: "`yaml:\"-\"`",
			want: FieldDoc{
				YAMLKey:   "-",
				IsIgnored: true,
				Required:  true,
			},
		},
		{
			name:     "yaml inline tag",
			tagValue: "`yaml:\",inline\"`",
			want: FieldDoc{
				YAMLKey:  "",
				IsInline: true,
				Required: true,
			},
		},
		{
			name:     "yaml and json tags",
			tagValue: "`yaml:\"instance\" json:\"instance\"`",
			want: FieldDoc{
				YAMLKey:  "instance",
				JSONKey:  "instance",
				Required: true,
			},
		},
		{
			name:     "yaml with multiple options",
			tagValue: "`yaml:\"name,omitempty,inline\"`",
			want: FieldDoc{
				YAMLKey:  "name",
				Required: false,
				IsInline: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FieldDoc{}
			parseTag(tt.tagValue, &got)

			if got.YAMLKey != tt.want.YAMLKey {
				t.Errorf("YAMLKey = %q, want %q", got.YAMLKey, tt.want.YAMLKey)
			}
			if got.JSONKey != tt.want.JSONKey {
				t.Errorf("JSONKey = %q, want %q", got.JSONKey, tt.want.JSONKey)
			}
			if got.Required != tt.want.Required {
				t.Errorf("Required = %v, want %v", got.Required, tt.want.Required)
			}
			if got.IsInline != tt.want.IsInline {
				t.Errorf("IsInline = %v, want %v", got.IsInline, tt.want.IsInline)
			}
			if got.IsIgnored != tt.want.IsIgnored {
				t.Errorf("IsIgnored = %v, want %v", got.IsIgnored, tt.want.IsIgnored)
			}
		})
	}
}

func TestTypeToString(t *testing.T) {
	// Create a temporary Go file to parse types from
	src := `package test

type Example struct {
	Simple     string
	Pointer    *string
	Slice      []string
	Map        map[string]string
	MapAny     map[string]any
	Nested     map[string][]string
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(tmpFile, []byte(src), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	structs, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("Expected 1 struct, got %d", len(structs))
	}

	tests := []struct {
		fieldName string
		wantType  string
	}{
		{"Simple", "string"},
		{"Pointer", "*string"},
		{"Slice", "[]string"},
		{"Map", "map[string]string"},
		{"MapAny", "map[string]any"},
		{"Nested", "map[string][]string"},
	}

	fields := make(map[string]FieldDoc)
	for _, f := range structs[0].Fields {
		fields[f.Name] = f
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			field, ok := fields[tt.fieldName]
			if !ok {
				t.Fatalf("Field %s not found", tt.fieldName)
			}
			if field.GoType != tt.wantType {
				t.Errorf("GoType = %q, want %q", field.GoType, tt.wantType)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	src := `package test

// Config represents the main configuration.
// It has multiple comment lines.
type Config struct {
	// Name is the project name.
	Name string ` + "`yaml:\"name\"`" + `

	// Region specifies the cloud region.
	Region string ` + "`yaml:\"region,omitempty\"`" + `

	// Ignored field should not appear in docs.
	Internal bool ` + "`yaml:\"-\"`" + `

	// NodeGroups defines the node pool configuration.
	NodeGroups map[string]NodeGroup ` + "`yaml:\"node_groups\"`" + `
}

// NodeGroup represents a single node group.
type NodeGroup struct {
	Instance string ` + "`yaml:\"instance\"`" + `
	MinNodes int    ` + "`yaml:\"min_nodes,omitempty\"`" + `
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.go")
	if err := os.WriteFile(tmpFile, []byte(src), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	structs, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(structs) != 2 {
		t.Fatalf("Expected 2 structs, got %d", len(structs))
	}

	// Test Config struct
	config := structs[0]
	if config.Name != "Config" {
		t.Errorf("Config.Name = %q, want %q", config.Name, "Config")
	}
	if !strings.Contains(config.Doc, "main configuration") {
		t.Errorf("Config.Doc should contain 'main configuration', got %q", config.Doc)
	}
	if len(config.Fields) != 4 {
		t.Errorf("Config should have 4 fields, got %d", len(config.Fields))
	}

	// Test field documentation
	fieldMap := make(map[string]FieldDoc)
	for _, f := range config.Fields {
		fieldMap[f.Name] = f
	}

	nameField := fieldMap["Name"]
	if nameField.YAMLKey != "name" {
		t.Errorf("Name.YAMLKey = %q, want %q", nameField.YAMLKey, "name")
	}
	if !nameField.Required {
		t.Error("Name should be required")
	}
	if !strings.Contains(nameField.Doc, "project name") {
		t.Errorf("Name.Doc should contain 'project name', got %q", nameField.Doc)
	}

	regionField := fieldMap["Region"]
	if regionField.Required {
		t.Error("Region should not be required (has omitempty)")
	}

	internalField := fieldMap["Internal"]
	if !internalField.IsIgnored {
		t.Error("Internal should be ignored")
	}

	// Test NodeGroup struct
	nodeGroup := structs[1]
	if nodeGroup.Name != "NodeGroup" {
		t.Errorf("NodeGroup.Name = %q, want %q", nodeGroup.Name, "NodeGroup")
	}
}

func TestParseFileWithInlineComment(t *testing.T) {
	src := `package test

type Config struct {
	Name   string ` + "`yaml:\"name\"`" + `   // The config name
	Region string ` + "`yaml:\"region\"`" + ` // AWS region for deployment
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.go")
	if err := os.WriteFile(tmpFile, []byte(src), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	structs, err := ParseFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("Expected 1 struct, got %d", len(structs))
	}

	fields := structs[0].Fields
	if len(fields) != 2 {
		t.Fatalf("Expected 2 fields, got %d", len(fields))
	}

	// Check inline comments are captured
	if !strings.Contains(fields[0].Doc, "config name") {
		t.Errorf("Name.Doc should contain 'config name', got %q", fields[0].Doc)
	}
	if !strings.Contains(fields[1].Doc, "AWS region") {
		t.Errorf("Region.Doc should contain 'AWS region', got %q", fields[1].Doc)
	}
}

func TestFilterConfigStructs(t *testing.T) {
	structs := []StructDoc{
		{Name: "Config"},
		{Name: "NodeGroup"},
		{Name: "Taint"},
		{Name: "Helper"},
	}

	tests := []struct {
		name   string
		filter []string
		want   []string
	}{
		{
			name:   "filter some",
			filter: []string{"Config", "NodeGroup"},
			want:   []string{"Config", "NodeGroup"},
		},
		{
			name:   "filter all",
			filter: []string{"Config", "NodeGroup", "Taint", "Helper"},
			want:   []string{"Config", "NodeGroup", "Taint", "Helper"},
		},
		{
			name:   "filter none",
			filter: []string{"NotExist"},
			want:   []string{},
		},
		{
			name:   "partial match",
			filter: []string{"Config", "NotExist"},
			want:   []string{"Config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterConfigStructs(structs, tt.filter)
			if len(got) != len(tt.want) {
				t.Errorf("FilterConfigStructs returned %d structs, want %d", len(got), len(tt.want))
				return
			}
			for i, s := range got {
				if s.Name != tt.want[i] {
					t.Errorf("got[%d].Name = %q, want %q", i, s.Name, tt.want[i])
				}
			}
		})
	}
}

func TestCleanComment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple comment",
			input: "This is a comment.\n",
			want:  "This is a comment.",
		},
		{
			name:  "multiline comment",
			input: "First line.\nSecond line.\n",
			want:  "First line.\nSecond line.",
		},
		{
			name:  "with leading/trailing whitespace",
			input: "  Comment text  \n\n",
			want:  "Comment text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanComment(tt.input)
			if got != tt.want {
				t.Errorf("cleanComment(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateOutputName(t *testing.T) {
	tests := []struct {
		name       string
		sourcePath string
		want       string
	}{
		{
			name:       "config package",
			sourcePath: "pkg/config/config.go",
			want:       "core.md",
		},
		{
			name:       "aws provider",
			sourcePath: "pkg/provider/aws/config.go",
			want:       "aws.md",
		},
		{
			name:       "gcp provider",
			sourcePath: "pkg/provider/gcp/config.go",
			want:       "gcp.md",
		},
		{
			name:       "cloudflare dns",
			sourcePath: "pkg/dnsprovider/cloudflare/config.go",
			want:       "cloudflare.md",
		},
		{
			name:       "git package",
			sourcePath: "pkg/git/config.go",
			want:       "git.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateOutputName(tt.sourcePath)
			if got != tt.want {
				t.Errorf("generateOutputName(%q) = %q, want %q", tt.sourcePath, got, tt.want)
			}
		})
	}
}
