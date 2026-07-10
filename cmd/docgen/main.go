//go:generate go run .

// Command docgen generates markdown documentation from Go config structs.
//
// Usage:
//
//	go run ./cmd/docgen -output docs/configuration
//
// This tool parses Go source files using go/ast to extract struct definitions,
// field types, yaml tags, and doc comments, then generates markdown documentation.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// providerGroups are the directories under pkg/providers/ that hold one
// subdirectory per provider, each with its own config.go. Discovered via
// glob rather than a hand-maintained list, so a new provider package or a
// new struct in an existing provider's config.go is picked up automatically
// instead of silently missing from the generated docs.
var providerGroups = []string{"cluster", "dns"}

// providerDocMeta holds human-authored title/description overrides for
// discovered provider config pages, keyed by the provider directory relative
// to the project root. A provider not listed here still gets a page, just
// with a generated title/description instead of curated prose.
var providerDocMeta = map[string]struct{ title, desc string }{
	"pkg/providers/cluster/aws":      {"AWS Provider Configuration", "Configuration options specific to Amazon Web Services (EKS)."},
	"pkg/providers/cluster/gcp":      {"GCP Provider Configuration", "Configuration options specific to Google Cloud Platform (GKE)."},
	"pkg/providers/cluster/azure":    {"Azure Provider Configuration", "Configuration options specific to Microsoft Azure (AKS)."},
	"pkg/providers/cluster/hetzner":  {"Hetzner Provider Configuration", "Configuration options specific to Hetzner Cloud."},
	"pkg/providers/cluster/local":    {"Local Provider Configuration", "Configuration options for local Kubernetes deployments."},
	"pkg/providers/cluster/existing": {"Existing Cluster Configuration", "Configuration options for attaching to an existing Kubernetes cluster."},
	"pkg/providers/dns/cloudflare":   {"Cloudflare DNS Configuration", "Configuration options for Cloudflare DNS provider."},
}

// configFile represents a source file and the structs to extract from it. A
// nil/empty structs list means "document every exported struct in the file,
// in source order" - used for discovered provider files so nothing has to be
// enumerated by hand.
type configFile struct {
	path     string
	structs  []string
	docTitle string
	docDesc  string
}

// configFiles lists the non-provider config files that mix documentation-
// worthy structs with internal ones (e.g. pkg/config/config.go has
// ValidateOptions and DNSConfig alongside NebariConfig), so an explicit
// allowlist is still needed here. Provider config files are discovered
// separately by discoverProviderConfigFiles.
var configFiles = []configFile{
	{
		path: "pkg/config/config.go",
		structs: []string{
			"NebariConfig",
			"CertificateConfig",
			"ACMEConfig",
			"ExistingSecretRef",
			"CertFiles",
			"CertEnv",
		},
		docTitle: "Core Configuration",
		docDesc:  "Core Nebari configuration options used by all providers.",
	},
	{
		path: "pkg/git/config.go",
		structs: []string{
			"Config",
			"AuthConfig",
		},
		docTitle: "Git Repository Configuration",
		docDesc:  "Configuration options for GitOps repository integration with ArgoCD.",
	},
}

// discoverProviderConfigFiles globs pkg/providers/{cluster,dns}/*/config.go
// and returns one configFile per match, sorted by path for deterministic
// output. Every match documents all of its exported structs; there is no way
// for a provider directory to be silently skipped or partially documented.
func discoverProviderConfigFiles(rootDir string) ([]configFile, error) {
	var discovered []configFile

	for _, group := range providerGroups {
		pattern := filepath.Join(rootDir, "pkg", "providers", group, "*", "config.go")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("globbing %s: %w", pattern, err)
		}

		for _, match := range matches {
			relPath, err := filepath.Rel(rootDir, match)
			if err != nil {
				return nil, fmt.Errorf("computing relative path for %s: %w", match, err)
			}
			relPath = filepath.ToSlash(relPath)
			providerDir := filepath.ToSlash(filepath.Dir(relPath))

			title, desc := providerDocMeta[providerDir].title, providerDocMeta[providerDir].desc
			if title == "" {
				name := filepath.Base(providerDir)
				title = strings.ToUpper(name[:1]) + name[1:] + " Provider Configuration"
				desc = fmt.Sprintf("Configuration options for the %s provider.", name)
			}

			discovered = append(discovered, configFile{path: relPath, docTitle: title, docDesc: desc})
		}
	}

	sort.Slice(discovered, func(i, j int) bool { return discovered[i].path < discovered[j].path })
	return discovered, nil
}

func main() {
	outputDir := flag.String("output", "docs/configuration", "Output directory for generated configuration documentation")
	cliOutputDir := flag.String("cli-output", "docs/reference/cli", "Output directory for generated CLI reference documentation")
	rootDir := flag.String("root", "", "Root directory of the project (defaults to current directory)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	if *rootDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		*rootDir = findProjectRoot(wd)
	}

	if *verbose {
		log.Printf("Project root: %s", *rootDir)
		log.Printf("Configuration output directory: %s", *outputDir)
		log.Printf("CLI output directory: %s", *cliOutputDir)
	}

	outPath := filepath.Join(*rootDir, *outputDir)
	if err := os.MkdirAll(outPath, 0750); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	providerFiles, err := discoverProviderConfigFiles(*rootDir)
	if err != nil {
		log.Fatalf("Failed to discover provider config files: %v", err)
	}
	if len(providerFiles) == 0 {
		log.Fatalf("No provider config files discovered under pkg/providers/{%s}/*/config.go", strings.Join(providerGroups, ","))
	}

	allConfigFiles := append(append([]configFile{}, configFiles...), providerFiles...)

	for _, cf := range allConfigFiles {
		if err := processConfigFile(*rootDir, outPath, cf, *verbose); err != nil {
			log.Fatalf("Failed to process %s: %v", cf.path, err)
		}
	}

	if err := generateIndex(outPath, allConfigFiles); err != nil {
		log.Fatalf("Failed to generate index: %v", err)
	}

	cliOutPath := filepath.Join(*rootDir, *cliOutputDir)
	if err := generateCLIDocs(cliOutPath); err != nil {
		log.Fatalf("Failed to generate CLI docs: %v", err)
	}

	fmt.Printf("Configuration documentation generated successfully in %s\n", outPath)
	fmt.Printf("CLI documentation generated successfully in %s\n", cliOutPath)
}

func processConfigFile(rootDir, outPath string, cf configFile, verbose bool) (err error) {
	srcPath := filepath.Join(rootDir, cf.path)

	if verbose {
		log.Printf("Parsing %s...", srcPath)
	}

	allStructs, err := ParseFile(srcPath)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", srcPath, err)
	}

	var ordered []StructDoc
	if len(cf.structs) > 0 {
		ordered = orderStructs(FilterConfigStructs(allStructs, cf.structs), cf.structs)
		if len(ordered) == 0 {
			return fmt.Errorf("no matching structs found in %s (looking for %v)", srcPath, cf.structs)
		}
	} else {
		ordered = exportedStructs(allStructs)
		if len(ordered) == 0 {
			return fmt.Errorf("no exported structs found in %s; this provider directory would yield no documentation", srcPath)
		}
	}

	outputName := generateOutputName(cf.path)
	outputPath := filepath.Join(outPath, outputName)

	if verbose {
		log.Printf("Writing %s...", outputPath)
	}

	f, err := os.Create(filepath.Clean(outputPath))
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	GenerateConfigDoc(f, cf.docTitle, cf.docDesc, ordered)

	return err
}

// exportedStructs returns every struct with an exported (capitalized) name,
// preserving source order.
func exportedStructs(structs []StructDoc) []StructDoc {
	var result []StructDoc
	for _, s := range structs {
		if s.Name != "" && unicode.IsUpper([]rune(s.Name)[0]) {
			result = append(result, s)
		}
	}
	return result
}

func orderStructs(structs []StructDoc, order []string) []StructDoc {
	structMap := make(map[string]StructDoc)
	for _, s := range structs {
		structMap[s.Name] = s
	}

	var result []StructDoc
	for _, name := range order {
		if s, ok := structMap[name]; ok {
			result = append(result, s)
		}
	}
	return result
}

func generateOutputName(sourcePath string) string {
	dir := filepath.Dir(sourcePath)
	base := filepath.Base(dir)

	switch base {
	case "config":
		return "core.md"
	default:
		return base + ".md"
	}
}

func findProjectRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// generateIndex writes README.md from the same configFiles list that was
// just processed into pages, so the index can never drift from - or hand-
// duplicate - the set of generated pages.
func generateIndex(outPath string, files []configFile) (err error) {
	indexPath := filepath.Join(outPath, "README.md")
	f, err := os.Create(filepath.Clean(indexPath))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	var core, cluster, other []configFile
	for _, cf := range files {
		switch {
		case cf.path == "pkg/config/config.go":
			core = append(core, cf)
		case strings.HasPrefix(cf.path, "pkg/providers/cluster/"):
			cluster = append(cluster, cf)
		default:
			other = append(other, cf)
		}
	}

	var b strings.Builder
	b.WriteString(`# Configuration Reference

This directory contains auto-generated documentation for Nebari Infrastructure Core configuration options.

> This documentation is auto-generated from source code using ` + "`go generate`" + `.
> To regenerate, run: ` + "`make docs`" + ` or ` + "`go generate ./cmd/docgen`" + `

## Configuration Files

### Core Configuration

`)
	writeIndexEntries(&b, core)

	b.WriteString("\n### Cloud Providers\n\n")
	writeIndexEntries(&b, cluster)

	b.WriteString("\n### Additional Configuration\n\n")
	writeIndexEntries(&b, other)

	_, err = f.WriteString(b.String())
	return err
}

func writeIndexEntries(b *strings.Builder, files []configFile) {
	for _, cf := range files {
		fmt.Fprintf(b, "- [%s](%s) - %s\n", cf.docTitle, generateOutputName(cf.path), cf.docDesc)
	}
}
