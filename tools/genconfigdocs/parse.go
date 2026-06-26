package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

// FieldDef describes one exported struct field.
type FieldDef struct {
	YAMLKey  string // YAML key name; empty when the field should be skipped
	GoType   string // formatted type string
	Required bool   // true when the yaml tag has no omitempty
	Inline   bool   // true when yaml:",inline"
	Doc      string // field-level doc comment; may be empty
}

// StructDef is a parsed exported struct type.
type StructDef struct {
	Name   string
	Doc    string
	Fields []FieldDef
}

// ParseFile parses the Go source file at path and returns every exported struct
// definition keyed by struct name.
func ParseFile(path string) (map[string]StructDef, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	result := make(map[string]StructDef)

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if !typeSpec.Name.IsExported() {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			sd := StructDef{
				Name: typeSpec.Name.Name,
				Doc:  extractCommentText(genDecl.Doc),
			}
			for _, field := range structType.Fields.List {
				sd.Fields = append(sd.Fields, parseField(field)...)
			}
			result[sd.Name] = sd
		}
	}

	return result, nil
}

func parseField(field *ast.Field) []FieldDef {
	if len(field.Names) == 0 {
		// Embedded / anonymous field — skip.
		return nil
	}
	if !field.Names[0].IsExported() {
		return nil
	}

	yamlKey, required, inline := parseYAMLTag(field.Tag)
	if yamlKey == "-" {
		return nil
	}
	if inline {
		return []FieldDef{{Inline: true}}
	}

	return []FieldDef{{
		YAMLKey:  yamlKey,
		GoType:   formatExpr(field.Type),
		Required: required,
		Inline:   false,
		Doc:      extractCommentText(field.Doc),
	}}
}

// parseYAMLTag returns the yaml key, whether the field is required (no omitempty),
// and whether the field is inline.
func parseYAMLTag(tag *ast.BasicLit) (key string, required bool, inline bool) {
	if tag == nil {
		return "", false, false
	}
	raw := strings.Trim(tag.Value, "`")
	yamlVal := reflect.StructTag(raw).Get("yaml")
	if yamlVal == "" {
		return "", false, false
	}
	parts := strings.Split(yamlVal, ",")
	key = parts[0]
	var hasOmitempty bool
	for _, opt := range parts[1:] {
		switch opt {
		case "omitempty":
			hasOmitempty = true
		case "inline":
			inline = true
		}
	}
	required = !hasOmitempty && !inline
	return key, required, inline
}

// formatExpr converts an ast.Expr into a human-readable type string.
func formatExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return formatIdent(t.Name)
	case *ast.StarExpr:
		return formatExpr(t.X) + " (optional)"
	case *ast.ArrayType:
		return "list of " + formatExpr(t.Elt)
	case *ast.MapType:
		return "map[string]" + formatExpr(t.Value)
	case *ast.SelectorExpr:
		pkg := ""
		if id, ok := t.X.(*ast.Ident); ok {
			pkg = id.Name
		}
		name := t.Sel.Name
		if pkg == "time" && name == "Duration" {
			return "duration"
		}
		return name
	case *ast.InterfaceType:
		return "any"
	default:
		return "unknown"
	}
}

func formatIdent(name string) string {
	switch name {
	case "string":
		return "string"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "integer"
	case "bool":
		return "boolean"
	case "float32", "float64":
		return "float"
	default:
		return name
	}
}

func extractCommentText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var parts []string
	for _, c := range cg.List {
		text := c.Text
		text = strings.TrimPrefix(text, "//")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}
