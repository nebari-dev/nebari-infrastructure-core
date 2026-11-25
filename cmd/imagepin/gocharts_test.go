package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const helmActionPath = "helm.sh/helm/v3/pkg/action"

// countInstallCalls counts calls to Helm's action.NewInstall in one Go file,
// under whatever name the file imports the action package as.
func countInstallCalls(path string) (int, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return 0, err
	}
	local := ""
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p != helmActionPath {
			continue
		}
		local = "action"
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" || local == "_" {
		return 0, nil
	}
	n := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewInstall" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == local {
				n++
			}
		}
		return true
	})
	return n, nil
}

// TestGoCharts_CoverEveryInstallSite fails when a chart NIC installs from Go
// code isn't listed in goCharts. Nothing else ties that list to the install
// sites, and a chart missing from it is left out of the inventory and the
// scan while the report still says it is complete.
func TestGoCharts_CoverEveryInstallSite(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	c := constReader{root: root}
	charts := goCharts(&c)
	if c.err != nil {
		t.Fatal(c.err)
	}
	listed := map[string]int{}
	for _, gc := range charts {
		listed[gc.defined]++
	}

	sites := map[string]int{}
	err = filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		n, err := countInstallCalls(path)
		if err != nil {
			return err
		}
		if n > 0 {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			sites[filepath.ToSlash(rel)] = n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) == 0 {
		t.Fatal("found no action.NewInstall calls under pkg/; the search itself is broken")
	}

	for file, n := range sites {
		if listed[file] != n {
			t.Errorf("%s calls action.NewInstall %d time(s), but goCharts lists %d chart(s) defined there; "+
				"add each chart it installs to goCharts in cmd/imagepin/gocharts.go, or its images are not inventoried or scanned",
				file, n, listed[file])
		}
	}
	for file, n := range listed {
		if sites[file] == 0 {
			t.Errorf("goCharts lists %d chart(s) defined in %s, but that file has no action.NewInstall call; "+
				"set defined to the file that installs the chart", n, file)
		}
	}
}

func TestCountInstallCalls(t *testing.T) {
	tests := []struct {
		name, src string
		want      int
	}{
		{"one call", `package p
import "helm.sh/helm/v3/pkg/action"
func f(c *action.Configuration) { _ = action.NewInstall(c) }`, 1},
		{"aliased import", `package p
import helmaction "helm.sh/helm/v3/pkg/action"
func f(c *helmaction.Configuration) { _ = helmaction.NewInstall(c); _ = helmaction.NewInstall(c) }`, 2},
		{"upgrade and comments don't count", `package p
import "helm.sh/helm/v3/pkg/action"
// action.NewInstall is mentioned here only.
func f(c *action.Configuration) { _ = action.NewUpgrade(c) }`, 0},
		{"another package's NewInstall", `package p
import "example.com/other/action"
func f() { _ = action.NewInstall() }`, 0},
		{"no import", `package p
func f() {}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.go")
			if err := os.WriteFile(path, []byte(tt.src), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := countInstallCalls(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("countInstallCalls = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDeclaresModule(t *testing.T) {
	tests := []struct {
		name, gomod string
		want        bool
	}{
		{"LF", "module " + modulePath + "\n\ngo 1.26\n", true},
		{"CRLF", "module " + modulePath + "\r\n\r\ngo 1.26\r\n", true},
		{"no trailing newline", "module " + modulePath, true},
		{"not first line", "// comment\nmodule " + modulePath + "\n", true},
		{"other module", "module example.com/other\n", false},
		{"prefix of another module", "module " + modulePath + "/sub\n", false},
		{"only mentioned", "require " + modulePath + " v1.0.0\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := declaresModule([]byte(tt.gomod), modulePath); got != tt.want {
				t.Errorf("declaresModule = %v, want %v", got, tt.want)
			}
		})
	}
}
