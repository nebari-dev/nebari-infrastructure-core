package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMarkdownGenerator_WriteHeader(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		wantTitle   bool
		wantDesc    bool
	}{
		{
			name:        "with description",
			title:       "Test Title",
			description: "Test description",
			wantTitle:   true,
			wantDesc:    true,
		},
		{
			name:        "without description",
			title:       "Test Title",
			description: "",
			wantTitle:   true,
			wantDesc:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			gen := NewMarkdownGenerator(&buf)
			gen.WriteHeader(tt.title, tt.description)

			output := buf.String()

			if tt.wantTitle && !strings.Contains(output, "# "+tt.title) {
				t.Errorf("output should contain title '# %s', got:\n%s", tt.title, output)
			}

			if tt.wantDesc && !strings.Contains(output, tt.description) {
				t.Errorf("output should contain description '%s', got:\n%s", tt.description, output)
			}

			if !tt.wantDesc && tt.description == "" && strings.Contains(output, "Test description") {
				t.Errorf("output should not contain description when empty")
			}

			// Should always contain auto-generated note
			if !strings.Contains(output, "auto-generated") {
				t.Errorf("output should contain auto-generated note")
			}
		})
	}
}

func TestMarkdownGenerator_WriteStruct(t *testing.T) {
	tests := []struct {
		name       string
		doc        StructDoc
		wantFields []string
		notWant    []string
	}{
		{
			name: "basic struct",
			doc: StructDoc{
				Name: "Config",
				Doc:  "Config is the main configuration.",
				Fields: []FieldDoc{
					{Name: "Region", GoType: "string", YAMLKey: "region", Required: true, Doc: "AWS region"},
					{Name: "Name", GoType: "string", YAMLKey: "name", Required: false, Doc: "Project name"},
				},
			},
			wantFields: []string{"Region", "Name", "`region`", "`name`", "AWS region", "Project name"},
		},
		{
			name: "struct with ignored field",
			doc: StructDoc{
				Name: "Config",
				Fields: []FieldDoc{
					{Name: "Public", GoType: "string", YAMLKey: "public", Required: true},
					{Name: "Private", GoType: "string", YAMLKey: "-", IsIgnored: true},
				},
			},
			wantFields: []string{"Public"},
			notWant:    []string{"Private"},
		},
		{
			name: "struct with inline field",
			doc: StructDoc{
				Name: "Config",
				Fields: []FieldDoc{
					{Name: "Public", GoType: "string", YAMLKey: "public", Required: true},
					{Name: "Extra", GoType: "map[string]any", IsInline: true},
				},
			},
			wantFields: []string{"Public"},
			notWant:    []string{"Extra"},
		},
		{
			name: "struct with pointer type",
			doc: StructDoc{
				Name: "Config",
				Fields: []FieldDoc{
					{Name: "Optional", GoType: "*string", YAMLKey: "optional", Required: true, Doc: "Optional field"},
				},
			},
			wantFields: []string{"Optional", "`*string`"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			gen := NewMarkdownGenerator(&buf)
			gen.WriteStruct(tt.doc, 2)

			output := buf.String()

			for _, want := range tt.wantFields {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q, got:\n%s", want, output)
				}
			}

			for _, notWant := range tt.notWant {
				if strings.Contains(output, notWant) {
					t.Errorf("output should NOT contain %q, got:\n%s", notWant, output)
				}
			}
		})
	}
}

func TestMarkdownGenerator_WriteTableOfContents(t *testing.T) {
	var buf bytes.Buffer
	gen := NewMarkdownGenerator(&buf)

	sections := []string{"Config", "Node Group", "Taint"}
	gen.WriteTableOfContents(sections)

	output := buf.String()

	// Check TOC header
	if !strings.Contains(output, "## Table of Contents") {
		t.Error("output should contain Table of Contents header")
	}

	// Check each section link
	expectedLinks := []string{
		"[Config](#config)",
		"[Node Group](#node-group)",
		"[Taint](#taint)",
	}

	for _, link := range expectedLinks {
		if !strings.Contains(output, link) {
			t.Errorf("output should contain link %q, got:\n%s", link, output)
		}
	}
}

func TestGenerateConfigDoc(t *testing.T) {
	structs := []StructDoc{
		{
			Name: "Config",
			Doc:  "Config is the main configuration struct.",
			Fields: []FieldDoc{
				{Name: "Region", GoType: "string", YAMLKey: "region", Required: true, Doc: "AWS region"},
			},
		},
		{
			Name: "NodeGroup",
			Doc:  "NodeGroup defines a node pool.",
			Fields: []FieldDoc{
				{Name: "Instance", GoType: "string", YAMLKey: "instance", Required: true},
			},
		},
	}

	var buf bytes.Buffer
	GenerateConfigDoc(&buf, "Test Config", "Test description", structs)

	output := buf.String()

	// Check header
	if !strings.Contains(output, "# Test Config") {
		t.Error("output should contain title")
	}

	// Check description
	if !strings.Contains(output, "Test description") {
		t.Error("output should contain description")
	}

	// Check TOC
	if !strings.Contains(output, "## Table of Contents") {
		t.Error("output should contain Table of Contents")
	}

	// Check both structs are documented
	if !strings.Contains(output, "## Config") {
		t.Error("output should contain Config heading")
	}
	if !strings.Contains(output, "## NodeGroup") {
		t.Error("output should contain NodeGroup heading")
	}

	// Check table structure
	if !strings.Contains(output, "| Field | YAML Key | Type | Required | Description |") {
		t.Error("output should contain table header")
	}
}

func TestFormatType(t *testing.T) {
	tests := []struct {
		name    string
		goType  string
		wantFmt string
	}{
		{
			name:    "simple type",
			goType:  "string",
			wantFmt: "string",
		},
		{
			name:    "pointer type",
			goType:  "*string",
			wantFmt: "`*string`",
		},
		{
			name:    "slice type",
			goType:  "[]string",
			wantFmt: "`[]string`",
		},
		{
			name:    "map type",
			goType:  "map[string]string",
			wantFmt: "`map[string]string`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatType(tt.goType)
			if got != tt.wantFmt {
				t.Errorf("formatType(%q) = %q, want %q", tt.goType, got, tt.wantFmt)
			}
		})
	}
}

func TestMarkdownGenerator_WriteSectionDivider(t *testing.T) {
	var buf bytes.Buffer
	gen := NewMarkdownGenerator(&buf)
	gen.WriteSectionDivider()

	if !strings.Contains(buf.String(), "---") {
		t.Error("section divider should contain ---")
	}
}

func TestMarkdownGenerator_WriteNote(t *testing.T) {
	var buf bytes.Buffer
	gen := NewMarkdownGenerator(&buf)
	gen.WriteNote("This is a test note")

	output := buf.String()
	if !strings.Contains(output, "> **Note:**") {
		t.Error("note should contain '> **Note:**'")
	}
	if !strings.Contains(output, "This is a test note") {
		t.Error("note should contain the note text")
	}
}
