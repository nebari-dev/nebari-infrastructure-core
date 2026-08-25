// Command starters renders the Nebi starter workspaces from examples/.
//
// A starter is the provider's example config with the identity-bearing values
// replaced by the CHANGEME sentinel, plus the pixi workspace that pins the
// toolchain and the provider's README. examples/ stays the single source of
// truth for config content, so there is no second copy to drift.
//
// Output is published as OCI bundles and deliberately not committed; see
// .github/workflows/starters.yml.
//
// Usage:
//
//	go run ./cmd/starters -out dist/starters      # or: make starters
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	outDir := flag.String("out", "dist/starters", "Output directory for the rendered starter workspaces")
	nicVersion := flag.String("version", "", "nic version the starters pin (default: the most recent git tag, minus the leading v)")
	templates := flag.String("templates", "starters/templates", "Directory holding pixi.toml.tmpl and the per-provider READMEs")
	examples := flag.String("examples", "examples", "Directory holding <provider>-config.yaml")
	flag.Parse()

	version := *nicVersion
	if version == "" {
		v, err := latestTag()
		if err != nil {
			log.Fatalf("could not determine the nic version to pin: %v; pass -version", err)
		}
		version = v
	}

	if err := generate(*outDir, *templates, *examples, version); err != nil {
		log.Fatalf("%v", err)
	}
}

// latestTag returns the most recent tag with its leading v stripped. On a tag
// build this is that tag; elsewhere it is the previous one, which is why the
// publish workflow is tag-only.
func latestTag() (string, error) {
	out, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
	if err != nil {
		return "", fmt.Errorf("git describe: %w", err)
	}
	tag := strings.TrimSpace(string(out))
	if tag == "" {
		return "", fmt.Errorf("git describe returned nothing")
	}
	return strings.TrimPrefix(tag, "v"), nil
}

// generate renders every provider in scope. Failures accumulate: a restructure
// of examples/ usually moves more than one key, and reporting them one run at
// a time makes the author rediscover the same problem repeatedly.
func generate(outDir, templates, examples, version string) error {
	pixiTmpl, err := os.ReadFile(filepath.Join(templates, "pixi.toml.tmpl"))
	if err != nil {
		return fmt.Errorf("read pixi template: %w", err)
	}

	var problems []string
	for _, name := range providerNames() {
		if err := generateOne(outDir, templates, examples, name, version, pixiTmpl); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		fmt.Printf("generated %s (nic %s)\n", filepath.Join(outDir, name), version)
	}

	if len(problems) > 0 {
		return fmt.Errorf("starter generation failed:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func generateOne(outDir, templates, examples, name, version string, pixiTmpl []byte) error {
	p := providers[name]

	src, err := os.ReadFile(filepath.Join(examples, name+"-config.yaml"))
	if err != nil {
		return fmt.Errorf("read example: %w", err)
	}

	config, err := placeholderConfig(src, p.fields)
	if err != nil {
		return err
	}

	pixi, err := renderPixi(pixiTmpl, name, version, p.deps)
	if err != nil {
		return err
	}

	readme, err := os.ReadFile(filepath.Join(templates, "README."+name+".md"))
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}

	dest := filepath.Join(outDir, name)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	for file, content := range map[string][]byte{
		"config.yaml": config,
		"pixi.toml":   pixi,
		"README.md":   readme,
	} {
		if err := os.WriteFile(filepath.Join(dest, file), content, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", file, err)
		}
	}
	return nil
}
