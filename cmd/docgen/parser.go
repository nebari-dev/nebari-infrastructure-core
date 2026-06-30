// Package main provides a documentation generator for config structs using go/ast.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

// StructDoc represents documentation for a Go struct.
type StructDoc struct {
	Name       string
	Doc        string
	Fields     []FieldDoc
	SourceFile string
}

// FieldDoc represents documentation for a struct field.
type FieldDoc struct {
	Name      string
	GoType    string
	YAMLKey   string
	JSONKey   string
	Required  bool
	Doc       string
	IsInline  bool
	IsIgnored bool // yaml:"-"
}

// ParseFile parses a Go source file and extracts struct documentation.
func ParseFile(filename string) ([]StructDoc, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var structs []StructDoc

	// Walk through all declarations
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			doc := StructDoc{
				Name:       typeSpec.Name.Name,
				SourceFile: filename,
			}

			// Get doc comment from either GenDecl or TypeSpec
			if genDecl.Doc != nil {
				doc.Doc = cleanComment(genDecl.Doc.Text())
			} else if typeSpec.Doc != nil {
				doc.Doc = cleanComment(typeSpec.Doc.Text())
			}

			// Parse fields
			for _, field := range structType.Fields.List {
				fieldDocs := parseField(field)
				doc.Fields = append(doc.Fields, fieldDocs...)
			}

			structs = append(structs, doc)
		}
	}

	return structs, nil
}

// parseField extracts documentation from a struct field.
func parseField(field *ast.Field) []FieldDoc {
	var docs []FieldDoc

	// Handle embedded/anonymous fields (no names)
	if len(field.Names) == 0 {
		// Embedded field - get the type name
		typeName := typeToString(field.Type)
		doc := FieldDoc{
			Name:   typeName,
			GoType: typeName,
		}

		if field.Tag != nil {
			parseTag(field.Tag.Value, &doc)
		}

		if field.Doc != nil {
			doc.Doc = cleanComment(field.Doc.Text())
		} else if field.Comment != nil {
			doc.Doc = cleanComment(field.Comment.Text())
		}

		docs = append(docs, doc)
		return docs
	}

	// Handle named fields
	for _, name := range field.Names {
		doc := FieldDoc{
			Name:   name.Name,
			GoType: typeToString(field.Type),
		}

		if field.Tag != nil {
			parseTag(field.Tag.Value, &doc)
		}

		// Get documentation from Doc comment (above field) or Comment (inline)
		if field.Doc != nil {
			doc.Doc = cleanComment(field.Doc.Text())
		} else if field.Comment != nil {
			doc.Doc = cleanComment(field.Comment.Text())
		}

		docs = append(docs, doc)
	}

	return docs
}

// parseTag parses struct tags and extracts yaml/json keys.
func parseTag(tagValue string, doc *FieldDoc) {
	// Remove backticks
	tagValue = strings.Trim(tagValue, "`")

	// Use reflect.StructTag to parse
	tag := reflect.StructTag(tagValue)

	// Parse yaml tag
	yamlTag := tag.Get("yaml")
	if yamlTag != "" {
		parts := strings.Split(yamlTag, ",")
		switch parts[0] {
		case "-":
			doc.IsIgnored = true
			doc.YAMLKey = "-"
		case "":
			// yaml:",inline" or similar - check for inline
			for _, opt := range parts[1:] {
				if opt == "inline" {
					doc.IsInline = true
				}
			}
		default:
			doc.YAMLKey = parts[0]
		}

		// Check for omitempty to determine required status
		doc.Required = true
		for _, opt := range parts {
			if opt == "omitempty" {
				doc.Required = false
			}
			if opt == "inline" {
				doc.IsInline = true
			}
		}
	} else {
		// No yaml tag - field name is used as-is (lowercase)
		doc.YAMLKey = strings.ToLower(doc.Name)
		doc.Required = true
	}

	// Parse json tag (for reference)
	jsonTag := tag.Get("json")
	if jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "-" && parts[0] != "" {
			doc.JSONKey = parts[0]
		}
	}
}

// typeToString converts an AST type expression to a readable string.
func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeToString(t.Elt)
		}
		return "[...]" + typeToString(t.Elt)
	case *ast.MapType:
		return "map[" + typeToString(t.Key) + "]" + typeToString(t.Value)
	case *ast.SelectorExpr:
		return typeToString(t.X) + "." + t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{...}"
	default:
		return "any"
	}
}

// cleanComment cleans up a comment string.
func cleanComment(s string) string {
	s = strings.TrimSpace(s)
	// Remove trailing newlines
	s = strings.TrimRight(s, "\n")
	return s
}

// FilterConfigStructs filters structs to only include those matching config types.
func FilterConfigStructs(structs []StructDoc, names []string) []StructDoc {
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	var result []StructDoc
	for _, s := range structs {
		if nameSet[s.Name] {
			result = append(result, s)
		}
	}
	return result
}
