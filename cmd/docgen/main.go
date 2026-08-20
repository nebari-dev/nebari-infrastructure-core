//go:generate go run .

// Command docgen generates NIC's reference documentation from the source of
// truth in Go, in three flavours:
//
//   - docs/configuration/  - markdown config reference, parsed out of the config
//     structs with go/ast (struct definitions, field types, yaml tags, doc comments)
//   - docs/reference/cli/  - markdown CLI reference, walked off internal/cli's cobra tree
//   - schemas/             - JSON Schema for nebari-config.yaml and each registered
//     provider, reflected off the live types via the provider registry
//
// Usage:
//
//	go run ./cmd/docgen              # all three
//	make docs                        # the same, plus cleaning stale output first
//
// It is an internal build/CI tool, not a user-facing subcommand of nic. All
// three outputs are committed in-tree and guarded by a CI drift check, so a
// config change that isn't accompanied by regenerated output fails the build.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// bareNameGroups are the provider categories whose pages are named after the
// provider alone (aws.md, cloudflare.md). They predate the third category and
// keep their existing filenames so published links don't break; every other
// category is qualified as <group>-<name>.md. Collisions are a build error
// either way - see checkOutputNameCollisions.
var bareNameGroups = map[string]bool{"cluster": true, "dns": true}

// discoverProviderGroups returns the category directories under pkg/providers/
// that hold at least one provider with a config.go.
//
// This is deliberately not a hand-maintained list. A literal []string{"cluster",
// "dns"} is what let pkg/providers/repository/ - a whole category with two
// providers - land on main with no generated documentation and a green docs
// gate, since a category nobody enumerates produces no pages and therefore no
// diff to fail on.
func discoverProviderGroups(rootDir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(rootDir, "pkg", "providers", "*", "*", "config.go"))
	if err != nil {
		return nil, fmt.Errorf("globbing provider groups: %w", err)
	}

	seen := map[string]bool{}
	var groups []string
	for _, match := range matches {
		group := filepath.Base(filepath.Dir(filepath.Dir(match)))
		if !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}
	}

	sort.Strings(groups)
	return groups, nil
}

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

	"pkg/providers/repository/local":    {"Local GitOps Repository Configuration", "Configuration options for the NIC-managed local GitOps repository ArgoCD syncs from."},
	"pkg/providers/repository/existing": {"Existing GitOps Repository Configuration", "Configuration options for pointing ArgoCD at a GitOps repository you already host."},
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
			"RepositoryConfig",
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
		// TrustBundleConfig is a top-level NebariConfig field that lives in its
		// own file, so config.go's allowlist above does not cover it; without
		// this entry core.md references *TrustBundleConfig with no page defining it.
		path: "pkg/config/trust_bundle.go",
		structs: []string{
			"TrustBundleConfig",
		},
		docTitle: "Trust Bundle Configuration",
		docDesc:  "Enterprise CA trust-bundle propagation to worker-node OS trust stores and, via trust-manager, into the cluster.",
	},
	{
		// BackupsConfig is a top-level NebariConfig field in its own file.
		// Empty structs means "document every exported struct in the file",
		// so BackupsConfig and its nested targets/schedules are all covered.
		path:     "pkg/config/backups.go",
		docTitle: "Backups Configuration",
		docDesc:  "Off-cluster backup scheduling for Longhorn volumes (S3/Azure targets, retention, keyless auth).",
	},
	{
		// longhorn.Config is referenced as *longhorn.Config from the AWS, Hetzner
		// and existing-cluster provider configs but lives outside pkg/config and
		// pkg/providers, so neither the allowlist above nor provider discovery
		// covers it. It carries the DedicatedNodes data-loss warning.
		path:     "pkg/storage/longhorn/config.go",
		docTitle: "Longhorn Storage Configuration",
		docDesc:  "Distributed block storage settings shared by the cloud providers, including dedicated-node scheduling.",
	},
}

// discoverProviderConfigFiles globs pkg/providers/{cluster,dns}/*/config.go
// and returns one configFile per match, sorted by path for deterministic
// output. Every match documents all of its exported structs; there is no way
// for a provider directory to be silently skipped or partially documented.
//
// Only config.go is globbed (not *.go), so a provider that splits its config
// across files won't have the extra file auto-discovered. That is caught rather
// than silent: validateDocumentedRefs fails the build when a documented struct
// references another struct in the same package with no page, which is exactly
// what a split-out config file would produce.
func discoverProviderConfigFiles(rootDir string, providerGroups []string) ([]configFile, error) {
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
				r := []rune(name)
				title = strings.ToUpper(string(r[0])) + string(r[1:]) + " Provider Configuration"
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
	schemaOutputDir := flag.String("schema-output", "schemas", "Output directory for generated JSON Schema documents")
	schemaProviders := flag.String("schema-providers", "", "Comma-separated provider subset to regenerate schemas for (default: all registered)")
	schemaVersion := flag.String("schema-version", "", "Version string stamped into schemas/manifest.json")
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
		log.Printf("Schema output directory: %s", *schemaOutputDir)
	}

	outPath := filepath.Join(*rootDir, *outputDir)
	if err := os.MkdirAll(outPath, 0750); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	providerGroups, err := discoverProviderGroups(*rootDir)
	if err != nil {
		log.Fatalf("Failed to discover provider groups: %v", err)
	}
	if len(providerGroups) == 0 {
		log.Fatalf("No provider categories discovered under pkg/providers/*/*/config.go")
	}
	if *verbose {
		log.Printf("Provider categories: %s", strings.Join(providerGroups, ", "))
	}

	providerFiles, err := discoverProviderConfigFiles(*rootDir, providerGroups)
	if err != nil {
		log.Fatalf("Failed to discover provider config files: %v", err)
	}
	if len(providerFiles) == 0 {
		log.Fatalf("No provider config files discovered under pkg/providers/{%s}/*/config.go", strings.Join(providerGroups, ","))
	}

	allConfigFiles := append(append([]configFile{}, configFiles...), providerFiles...)

	if err := checkOutputNameCollisions(allConfigFiles); err != nil {
		log.Fatalf("Documentation gap: %v", err)
	}

	var allRendered []StructDoc
	for _, cf := range allConfigFiles {
		rendered, err := processConfigFile(*rootDir, outPath, cf, *verbose)
		if err != nil {
			log.Fatalf("Failed to process %s: %v", cf.path, err)
		}
		allRendered = append(allRendered, rendered...)
	}

	// Fail if a documented struct references another documentable struct that
	// has no generated page. This generalizes the guarantee provider discovery
	// already gives to the hand-maintained config allowlist, so a new
	// *XxxConfig field can't ship a dangling reference with a green docs gate.
	if err := validateDocumentedRefs(allRendered, *rootDir); err != nil {
		log.Fatalf("Documentation gap: %v", err)
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

	// Schemas are reflected off the live types rather than parsed, so this
	// emitter shares only the project root with the two above.
	schemaOutPath := filepath.Join(*rootDir, *schemaOutputDir)
	if err := generateSchemas(context.Background(), *rootDir, schemaOutPath, *schemaProviders, *schemaVersion); err != nil {
		log.Fatalf("Failed to generate schemas: %v", err)
	}
}

// processConfigFile parses cf's source, writes its page, and returns the
// structs it rendered so the caller can validate cross-references across the
// full documented set (see validateDocumentedRefs).
func processConfigFile(rootDir, outPath string, cf configFile, verbose bool) (rendered []StructDoc, err error) {
	srcPath := filepath.Join(rootDir, cf.path)

	if verbose {
		log.Printf("Parsing %s...", srcPath)
	}

	allStructs, err := ParseFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", srcPath, err)
	}

	if len(cf.structs) > 0 {
		rendered = orderStructs(FilterConfigStructs(allStructs, cf.structs), cf.structs)
		if len(rendered) == 0 {
			return nil, fmt.Errorf("no matching structs found in %s (looking for %v)", srcPath, cf.structs)
		}
	} else {
		rendered = exportedStructs(allStructs)
		if len(rendered) == 0 {
			return nil, fmt.Errorf("no exported structs found in %s; this config file would yield no documentation", srcPath)
		}
	}

	outputName := generateOutputName(cf.path)
	outputPath := filepath.Join(outPath, outputName)

	if verbose {
		log.Printf("Writing %s...", outputPath)
	}

	f, err := os.Create(filepath.Clean(outputPath))
	if err != nil {
		return nil, fmt.Errorf("creating output file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	GenerateConfigDoc(f, cf.docTitle, cf.docDesc, rendered)

	return rendered, err
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

	// pkg/providers/<group>/<name>/config.go: qualify the page with its
	// category unless the category predates the naming rule, so that
	// cluster/local and repository/local don't both claim local.md - which
	// would silently overwrite one with the other.
	if rel := filepath.ToSlash(dir); strings.HasPrefix(rel, "pkg/providers/") {
		parts := strings.Split(strings.TrimPrefix(rel, "pkg/providers/"), "/")
		if len(parts) == 2 && !bareNameGroups[parts[0]] {
			return parts[0] + "-" + parts[1] + ".md"
		}
	}

	switch base {
	case "config":
		// pkg/config/ holds more than one documented file (config.go plus
		// e.g. trust_bundle.go), so page names must key on the file, not just
		// the directory, or the second file silently overwrites core.md.
		file := strings.TrimSuffix(filepath.Base(sourcePath), ".go")
		if file == "config" {
			return "core.md"
		}
		return strings.ReplaceAll(file, "_", "-") + ".md"
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

// checkOutputNameCollisions fails when two source files would be written to the
// same page. Nothing downstream notices otherwise: the second write overwrites
// the first, the index still lists both, and the drift gate sees a consistent
// tree. pkg/config hit this once already (trust_bundle.go silently replacing
// core.md), and adding a provider category makes it reachable again via
// same-named providers in different categories.
func checkOutputNameCollisions(files []configFile) error {
	claimed := map[string]string{} // output page -> source path that claimed it

	var clashes []string
	for _, cf := range files {
		name := generateOutputName(cf.path)
		if prev, ok := claimed[name]; ok {
			clashes = append(clashes, fmt.Sprintf("%s is claimed by both %s and %s", name, prev, cf.path))
			continue
		}
		claimed[name] = cf.path
	}

	if len(clashes) > 0 {
		sort.Strings(clashes)
		return fmt.Errorf("generated pages collide:\n  - %s", strings.Join(clashes, "\n  - "))
	}
	return nil
}
