package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/parser"
)

// The example a starter is rendered from, in miniature: nested keys, a quoted
// value, a trailing comment on a value, a standalone comment, and a blank line.
const sample = `project_name: my-nebari
# how the platform is reached
domain: nebari.example.com

repository:
  existing:
    url: "git@github.com:my-org/my-gitops.git"
    path: "clusters/my-nebari"  # Optional subdirectory
    branch: main
`

func TestPlaceholderConfigPreservesEverythingButTheValue(t *testing.T) {
	got, err := placeholderConfig([]byte(sample), []string{
		"$.project_name",
		"$.repository.existing.path",
	})
	if err != nil {
		t.Fatalf("placeholderConfig: %v", err)
	}

	want := `project_name: CHANGEME
# how the platform is reached
domain: nebari.example.com

repository:
  existing:
    url: "git@github.com:my-org/my-gitops.git"
    path: CHANGEME  # Optional subdirectory
    branch: main
`
	if string(got) != want {
		t.Errorf("rendered config mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The reason this is a Go command and not a text substitution: a key that is
// renamed or moved resolves to nothing, loudly, instead of leaving a real
// value in a starter that then ships.
func TestPlaceholderConfigFailsOnAPathThatNoLongerResolves(t *testing.T) {
	_, err := placeholderConfig([]byte(sample), []string{"$.repository.existing.repo_url"})
	if err == nil {
		t.Fatal("want an error for a path that resolves to nothing, got nil")
	}
	if !strings.Contains(err.Error(), "repo_url") {
		t.Errorf("the error should name the path that failed, got: %v", err)
	}
}

// A same-named key at another level is the case a line-prefix match got wrong:
// "    path: " matched on indentation alone, so an unrelated four-space path:
// key was indistinguishable from the GitOps one. A YAML path is not.
func TestPlaceholderConfigTargetsTheRightKeyAmongSameNamedSiblings(t *testing.T) {
	src := `path: /not/this/one
trust_bundle:
    path: /nor/this/one
repository:
  existing:
    path: "clusters/my-nebari"
`
	got, err := placeholderConfig([]byte(src), []string{"$.repository.existing.path"})
	if err != nil {
		t.Fatalf("placeholderConfig: %v", err)
	}

	want := `path: /not/this/one
trust_bundle:
    path: /nor/this/one
repository:
  existing:
    path: CHANGEME
`
	if string(got) != want {
		t.Errorf("wrong key placeholdered\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestPlaceholderConfigRejectsMultiLineValues(t *testing.T) {
	src := "trust_bundle:\n  inline: |\n    -----BEGIN CERTIFICATE-----\n    abc\n"
	_, err := placeholderConfig([]byte(src), []string{"$.trust_bundle.inline"})
	if err == nil {
		t.Fatal("want an error for a block scalar, got nil")
	}
	if !strings.Contains(err.Error(), "multi-line") {
		t.Errorf("error should say why a block scalar cannot be edited in place, got: %v", err)
	}
}

func TestPlaceholderConfigRequiresDeclaredFields(t *testing.T) {
	if _, err := placeholderConfig([]byte(sample), nil); err == nil {
		t.Fatal("a provider with no declared fields must be an error, not a starter with every real value intact")
	}
}

func TestPlaceholderConfigOutputStillParses(t *testing.T) {
	got, err := placeholderConfig([]byte(sample), []string{"$.project_name", "$.repository.existing.url"})
	if err != nil {
		t.Fatalf("placeholderConfig: %v", err)
	}
	if _, err := parser.ParseBytes(got, parser.ParseComments); err != nil {
		t.Fatalf("rendered config does not parse: %v", err)
	}
}

func TestRenderPixi(t *testing.T) {
	tmpl := []byte("name = \"nebari-__PROVIDER__\"\nnic = \"__NIC_VERSION__\"\n__PROVIDER_DEPS__\n")

	got, err := renderPixi(tmpl, "aws", "0.13.0", `opentofu = ">=1.11.3,<2"`)
	if err != nil {
		t.Fatalf("renderPixi: %v", err)
	}
	want := "name = \"nebari-aws\"\nnic = \"0.13.0\"\nopentofu = \">=1.11.3,<2\"\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderPixiRejectsAnUnsubstitutedToken(t *testing.T) {
	_, err := renderPixi([]byte("channel = \"__REGISTRY__\"\n"), "aws", "0.13.0", "")
	if err == nil {
		t.Fatal("want an error for a token the renderer does not know, got nil")
	}
}

// The guard Marcelo's review asked for: this fails at `go test` time, on a
// laptop, rather than only once CI has built nic and rendered a starter.
func TestDeclaredFieldsResolveAgainstTheRealExamples(t *testing.T) {
	for _, name := range providerNames() {
		t.Run(name, func(t *testing.T) {
			p := providers[name]
			if len(p.fields) == 0 {
				t.Fatalf("%s declares no placeholder fields", name)
			}

			src, err := os.ReadFile(filepath.Join("..", "..", "examples", name+"-config.yaml"))
			if err != nil {
				t.Fatalf("read example: %v", err)
			}

			got, err := placeholderConfig(src, p.fields)
			if err != nil {
				t.Fatalf("every declared field must resolve against examples/%s-config.yaml: %v", name, err)
			}

			// One CHANGEME per declared field, and the example must not have
			// carried the sentinel already - examples/ validate as-is.
			if n := strings.Count(string(got), placeholder); n != len(p.fields) {
				t.Errorf("got %d %s occurrences, want %d (one per declared field)", n, placeholder, len(p.fields))
			}
		})
	}
}
