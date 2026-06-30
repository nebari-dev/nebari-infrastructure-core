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

const nodeGroup = "NodeGroup"

// configFile represents a source file and the structs to extract from it.
type configFile struct {
	path     string
	structs  []string
	docTitle string
	docDesc  string
}

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
		path: "pkg/providers/cluster/aws/config.go",
		structs: []string{
			"Config",
			nodeGroup,
			"Taint",
			"EFSConfig",
			"AWSLoadBalancerControllerConfig",
			"ClusterAutoscalerConfig",
			"TrustBundleConfig",
		},
		docTitle: "AWS Provider Configuration",
		docDesc:  "Configuration options specific to Amazon Web Services (EKS).",
	},
	{
		path: "pkg/providers/cluster/gcp/config.go",
		structs: []string{
			"Config",
			nodeGroup,
			"Taint",
			"GuestAccelerator",
		},
		docTitle: "GCP Provider Configuration",
		docDesc:  "Configuration options specific to Google Cloud Platform (GKE).",
	},
	{
		path: "pkg/providers/cluster/azure/config.go",
		structs: []string{
			"Config",
			"NetworkConfig",
			nodeGroup,
		},
		docTitle: "Azure Provider Configuration",
		docDesc:  "Configuration options specific to Microsoft Azure (AKS).",
	},
	{
		path: "pkg/providers/cluster/hetzner/config.go",
		structs: []string{
			"Config",
			nodeGroup,
			"Autoscaling",
			"NetworkConfig",
			"SSHConfig",
		},
		docTitle: "Hetzner Provider Configuration",
		docDesc:  "Configuration options specific to Hetzner Cloud.",
	},
	{
		path: "pkg/providers/cluster/local/config.go",
		structs: []string{
			"Config",
			"MetalLBConfig",
		},
		docTitle: "Local Provider Configuration",
		docDesc:  "Configuration options for local Kubernetes deployments.",
	},
	{
		path: "pkg/providers/cluster/existing/config.go",
		structs: []string{
			"Config",
		},
		docTitle: "Existing Cluster Configuration",
		docDesc:  "Configuration options for attaching to an existing Kubernetes cluster.",
	},
	{
		path: "pkg/providers/dns/cloudflare/config.go",
		structs: []string{
			"Config",
		},
		docTitle: "Cloudflare DNS Configuration",
		docDesc:  "Configuration options for Cloudflare DNS provider.",
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

func main() {
	outputDir := flag.String("output", "docs/configuration", "Output directory for generated documentation")
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
		log.Printf("Output directory: %s", *outputDir)
	}

	outPath := filepath.Join(*rootDir, *outputDir)
	if err := os.MkdirAll(outPath, 0750); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	for _, cf := range configFiles {
		if err := processConfigFile(*rootDir, outPath, cf, *verbose); err != nil {
			log.Fatalf("Failed to process %s: %v", cf.path, err)
		}
	}

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

	allStructs, err := ParseFile(srcPath)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", srcPath, err)
	}

	structs := FilterConfigStructs(allStructs, cf.structs)
	if len(structs) == 0 {
		return fmt.Errorf("no matching structs found in %s (looking for %v)", srcPath, cf.structs)
	}

	ordered := orderStructs(structs, cf.structs)

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
> To regenerate, run: ` + "`make config-docs`" + ` or ` + "`go generate ./cmd/docgen`" + `

## Configuration Files

### Core Configuration

- [Core Configuration](core.md) - Main Nebari configuration (project name, provider, domain, certificates)

### Cloud Providers

- [AWS Configuration](aws.md) - Amazon Web Services (EKS) provider options
- [GCP Configuration](gcp.md) - Google Cloud Platform (GKE) provider options
- [Azure Configuration](azure.md) - Microsoft Azure (AKS) provider options
- [Hetzner Configuration](hetzner.md) - Hetzner Cloud provider options
- [Local Configuration](local.md) - Local Kubernetes provider options
- [Existing Cluster Configuration](existing.md) - Attach to an existing Kubernetes cluster

### Additional Configuration

- [Cloudflare DNS](cloudflare.md) - Cloudflare DNS provider configuration
- [Git Repository](git.md) - GitOps repository configuration for ArgoCD
`
	_, err = f.WriteString(content)
	return err
}
