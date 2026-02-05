package main

import (
	"fmt"
	"io"
	"strings"
)

// MarkdownGenerator generates markdown documentation from struct docs.
type MarkdownGenerator struct {
	w io.Writer
}

// NewMarkdownGenerator creates a new markdown generator.
func NewMarkdownGenerator(w io.Writer) *MarkdownGenerator {
	return &MarkdownGenerator{w: w}
}

// WriteHeader writes the document header.
func (g *MarkdownGenerator) WriteHeader(title, description string) {
	g.printf("# %s\n\n", title)
	if description != "" {
		g.printf("%s\n\n", description)
	}
	g.printf("> This documentation is auto-generated from source code using `go generate`.\n\n")
}

// printf writes formatted output, ignoring errors (doc generation is best-effort).
func (g *MarkdownGenerator) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(g.w, format, args...)
}

// WriteStruct writes documentation for a single struct.
func (g *MarkdownGenerator) WriteStruct(doc StructDoc, headingLevel int) {
	heading := strings.Repeat("#", headingLevel)
	g.printf("%s %s\n\n", heading, doc.Name)

	if doc.Doc != "" {
		g.printf("%s\n\n", doc.Doc)
	}

	// Filter out ignored fields and inline fields
	var visibleFields []FieldDoc
	for _, f := range doc.Fields {
		if !f.IsIgnored && !f.IsInline {
			visibleFields = append(visibleFields, f)
		}
	}

	if len(visibleFields) == 0 {
		g.printf("_No documented fields._\n\n")
		return
	}

	// Write table header
	g.printf("| Field | YAML Key | Type | Required | Description |\n")
	g.printf("|-------|----------|------|----------|-------------|\n")

	// Write each field
	for _, field := range visibleFields {
		required := "No"
		if field.Required && !strings.HasPrefix(field.GoType, "*") {
			// Required unless it's a pointer type (pointers are optional)
			required = "Yes"
		}
		g.writeFieldRow(field, required)
	}

	g.printf("\n")
}

func (g *MarkdownGenerator) writeFieldRow(field FieldDoc, required string) {
	// Escape pipe characters in descriptions
	desc := strings.ReplaceAll(field.Doc, "|", "\\|")
	// Replace newlines with spaces
	desc = strings.ReplaceAll(desc, "\n", " ")
	// Truncate long descriptions
	if len(desc) > 200 {
		desc = desc[:197] + "..."
	}

	yamlKey := field.YAMLKey
	if yamlKey == "" {
		yamlKey = strings.ToLower(field.Name)
	}

	// Format type for readability
	goType := formatType(field.GoType)

	g.printf("| %s | `%s` | %s | %s | %s |\n",
		field.Name, yamlKey, goType, required, desc)
}

// formatType formats a Go type for markdown display.
func formatType(t string) string {
	// Wrap complex types in code blocks
	if strings.Contains(t, "[") || strings.Contains(t, "*") {
		return "`" + t + "`"
	}
	return t
}

// WriteTableOfContents writes a table of contents for multiple sections.
func (g *MarkdownGenerator) WriteTableOfContents(sections []string) {
	g.printf("## Table of Contents\n\n")
	for _, section := range sections {
		anchor := strings.ToLower(section)
		anchor = strings.ReplaceAll(anchor, " ", "-")
		g.printf("- [%s](#%s)\n", section, anchor)
	}
	g.printf("\n")
}

// WriteSectionDivider writes a horizontal rule for section separation.
func (g *MarkdownGenerator) WriteSectionDivider() {
	g.printf("---\n\n")
}

// WriteNote writes a note/callout.
func (g *MarkdownGenerator) WriteNote(note string) {
	g.printf("> **Note:** %s\n\n", note)
}

// GenerateConfigDoc generates a complete configuration documentation file.
func GenerateConfigDoc(w io.Writer, title, description string, structDocs []StructDoc) {
	gen := NewMarkdownGenerator(w)
	gen.WriteHeader(title, description)

	// Generate TOC
	var sections []string
	for _, doc := range structDocs {
		sections = append(sections, doc.Name)
	}
	gen.WriteTableOfContents(sections)

	gen.WriteSectionDivider()

	// Generate struct documentation
	for i, doc := range structDocs {
		gen.WriteStruct(doc, 2)
		if i < len(structDocs)-1 {
			gen.WriteSectionDivider()
		}
	}
}
