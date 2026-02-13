//go:generate go run . -output ../../docs/configuration

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
)

// configFile represents a source file and the structs to extract from it.
type configFile struct {
	path     string
	structs  []string
	docTitle string
	docDesc  string
}

var configFiles = []configFile{
	{
		path:     "pkg/config/config.go",
		structs:  []string{"NebariConfig", "CertificateConfig", "ACMEConfig"},
		docTitle: "Core Configuration",
		docDesc:  "Core Nebari configuration options used by all providers.",
	},
	{
		path:     "pkg/provider/aws/config.go",
		structs:  []string{"Config", "NodeGroup", "Taint", "EFSConfig"},
		docTitle: "AWS Provider Configuration",
		docDesc:  "Configuration options specific to Amazon Web Services (EKS).",
	},
	{
		path:     "pkg/provider/gcp/config.go",
		structs:  []string{"Config", "NodeGroup", "Taint", "GuestAccelerator"},
		docTitle: "GCP Provider Configuration",
		docDesc:  "Configuration options specific to Google Cloud Platform (GKE).",
	},
	{
		path:     "pkg/provider/azure/config.go",
		structs:  []string{"Config", "NodeGroup", "Taint"},
		docTitle: "Azure Provider Configuration",
		docDesc:  "Configuration options specific to Microsoft Azure (AKS).",
	},
	{
		path:     "pkg/provider/local/config.go",
		structs:  []string{"Config"},
		docTitle: "Local Provider Configuration",
		docDesc:  "Configuration options for local Kubernetes (K3s) deployments.",
	},
	{
		path:     "pkg/dnsprovider/cloudflare/config.go",
		structs:  []string{"Config"},
		docTitle: "Cloudflare DNS Configuration",
		docDesc:  "Configuration options for Cloudflare DNS provider.",
	},
	{
		path:     "pkg/git/config.go",
		structs:  []string{"Config", "AuthConfig"},
		docTitle: "Git Repository Configuration",
		docDesc:  "Configuration options for GitOps repository integration with ArgoCD.",
	},
}

func main() {
	outputDir := flag.String("output", "docs/configuration", "Output directory for generated documentation")
	rootDir := flag.String("root", "", "Root directory of the project (defaults to current directory)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	if *rootDir == "" {
		// Try to find the project root by looking for go.mod
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		*rootDir = findProjectRoot(wd)
	}

	if *verbose {
		log.Printf("Project root: %s", *rootDir)
		log.Printf("Output directory: %s", *outputDir)
	}

	// Create output directory
	outPath := filepath.Join(*rootDir, *outputDir)
	if err := os.MkdirAll(outPath, 0750); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Process each config file
	for _, cf := range configFiles {
		if err := processConfigFile(*rootDir, outPath, cf, *verbose); err != nil {
			log.Fatalf("Failed to process %s: %v", cf.path, err)
		}
	}

	// Generate index file
	if err := generateIndex(outPath); err != nil {
		log.Fatalf("Failed to generate index: %v", err)
	}

	fmt.Printf("Documentation generated successfully in %s\n", outPath)
}

func processConfigFile(rootDir, outPath string, cf configFile, verbose bool) error {
	srcPath := filepath.Join(rootDir, cf.path)

	if verbose {
		log.Printf("Parsing %s...", srcPath)
	}

	// Parse the source file
	allStructs, err := ParseFile(srcPath)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", srcPath, err)
	}

	// Filter to only the structs we want
	structs := FilterConfigStructs(allStructs, cf.structs)
	if len(structs) == 0 {
		return fmt.Errorf("no matching structs found in %s (looking for %v)", srcPath, cf.structs)
	}

	// Order structs according to the order in cf.structs
	ordered := orderStructs(structs, cf.structs)

	// Generate output filename from source file
	outputName := generateOutputName(cf.path)
	outputPath := filepath.Join(outPath, outputName)

	if verbose {
		log.Printf("Writing %s...", outputPath)
	}

	// Create output file
	f, err := os.Create(filepath.Clean(outputPath))
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// Generate documentation
	GenerateConfigDoc(f, cf.docTitle, cf.docDesc, ordered)

	return err
}

// orderStructs orders structs according to the desired order.
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

// generateOutputName generates an output filename from a source path.
func generateOutputName(sourcePath string) string {
	// Extract meaningful parts from the path
	// e.g., "pkg/provider/aws/config.go" -> "aws.md"
	// e.g., "pkg/config/config.go" -> "core.md"
	// e.g., "pkg/dnsprovider/cloudflare/config.go" -> "cloudflare.md"
	// e.g., "pkg/git/config.go" -> "git.md"

	dir := filepath.Dir(sourcePath)
	base := filepath.Base(dir)

	// Handle special cases
	switch base {
	case "config":
		return "core.md"
	default:
		return base + ".md"
	}
}

// findProjectRoot walks up the directory tree to find go.mod.
func findProjectRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return start
		}
		dir = parent
	}
}

// generateIndex creates an index.md file linking to all generated docs.
func generateIndex(outPath string) (err error) {
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

	content := `# Configuration Reference

This directory contains auto-generated documentation for Nebari Infrastructure Core configuration options.

> This documentation is auto-generated from source code using ` + "`go generate`" + `.
> To regenerate, run: ` + "`make docs`" + ` or ` + "`go generate ./cmd/docgen`" + `

## Configuration Files

### Core Configuration

- [Core Configuration](core.md) - Main Nebari configuration (project name, provider, domain)

### Cloud Providers

- [AWS Configuration](aws.md) - Amazon Web Services (EKS) provider options
- [GCP Configuration](gcp.md) - Google Cloud Platform (GKE) provider options
- [Azure Configuration](azure.md) - Microsoft Azure (AKS) provider options
- [Local Configuration](local.md) - Local Kubernetes (K3s) provider options

### Additional Configuration

- [Cloudflare DNS](cloudflare.md) - Cloudflare DNS provider configuration
- [Git Repository](git.md) - GitOps repository configuration for ArgoCD

## Example Configuration

See the [examples](../../examples/) directory for complete configuration examples.
`
	_, err = f.WriteString(content)
	return err
}
