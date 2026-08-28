package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// structsDeliberatelyUndocumented are struct types referenced from a documented
// struct that intentionally get no page of their own, keyed as
// "<packageDir>.<Name>". ClusterConfig and DNSConfig are one-field,
// provider-agnostic wrappers whose real content lives on the per-provider
// pages, so a dedicated page would be vacuous.
var structsDeliberatelyUndocumented = map[string]bool{
	"config.ClusterConfig": true,
	"config.DNSConfig":     true,
}

// exportedIdentRE pulls exported type identifiers out of a rendered Go type
// string (e.g. "*BackupsConfig", "[]Taint", "map[string]*NodeGroup",
// "*longhorn.Config"). The optional leading "pkg." group distinguishes a
// same-package reference (no qualifier) from a cross-package one.
var exportedIdentRE = regexp.MustCompile(`(?:([a-z]\w*)\.)?([A-Z]\w*)`)

type typeRef struct {
	pkg  string // import qualifier as written; "" for a same-package reference
	name string
}

func refsInGoType(goType string) []typeRef {
	var refs []typeRef
	for _, m := range exportedIdentRE.FindAllStringSubmatch(goType, -1) {
		refs = append(refs, typeRef{pkg: m[1], name: m[2]})
	}
	return refs
}

// pkgDir returns the package directory name for a source file path,
// e.g. ".../pkg/config/backups.go" -> "config".
func pkgDir(sourceFile string) string {
	return filepath.Base(filepath.Dir(sourceFile))
}

// validateDocumentedRefs fails when a documented struct has a field that
// references, within the same package, another struct defined in that package
// but with no generated section. This is exactly the gap that shipped more than
// once (a top-level *XxxConfig field, or a provider config split across files):
// the hand-maintained allowlist missed a struct a documented type points at, and
// `make docs` stayed green because the absent page produced no diff.
//
// Cross-package references (e.g. *longhorn.Config) are documented by adding an
// explicit configFiles entry; guarding those fully would mean resolving each
// selector's import path, which is intentionally out of scope here. The
// same-package check covers the realistic recurrence: a new struct added to an
// already-documented package (pkg/config or a provider dir) and referenced from
// a documented type.
func validateDocumentedRefs(rendered []StructDoc, rootDir string) error {
	documented := map[string]bool{}              // "pkg.Name" -> has a page
	definedInPkg := map[string]map[string]bool{} // pkg -> set of struct names declared in it

	dirs := map[string]bool{}
	for _, s := range rendered {
		documented[pkgDir(s.SourceFile)+"."+s.Name] = true
		dirs[filepath.Dir(s.SourceFile)] = true
	}
	for dir := range dirs {
		names, err := structNamesInDir(dir)
		if err != nil {
			return fmt.Errorf("scanning package %s: %w", dir, err)
		}
		definedInPkg[filepath.Base(dir)] = names
	}

	var gaps []string
	seen := map[string]bool{}
	for _, s := range rendered {
		pkg := pkgDir(s.SourceFile)
		for _, f := range s.Fields {
			for _, r := range refsInGoType(f.GoType) {
				if r.pkg != "" {
					continue // cross-package: covered by explicit configFiles entries
				}
				if !definedInPkg[pkg][r.name] {
					continue // not a struct declared in this package (primitive, external, non-struct type)
				}
				q := pkg + "." + r.name
				if documented[q] || structsDeliberatelyUndocumented[q] || seen[q] {
					continue
				}
				seen[q] = true
				gaps = append(gaps, fmt.Sprintf("%s (referenced by %s.%s field %q) has no generated page; add its file to configFiles or, if intentional, to structsDeliberatelyUndocumented", q, pkg, s.Name, f.Name))
			}
		}
	}
	if len(gaps) > 0 {
		sort.Strings(gaps)
		return fmt.Errorf("documented config structs reference structs with no page:\n  - %s", strings.Join(gaps, "\n  - "))
	}
	return nil
}

// structNamesInDir returns the set of struct type names declared in the
// non-test Go files of dir.
func structNamesInDir(dir string) (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		structs, err := ParseFile(m)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", m, err)
		}
		for _, s := range structs {
			names[s.Name] = true
		}
	}
	return names, nil
}
