// Package argocd generates ArgoCD Application manifests for Nebari's
// foundational software stack.
package argocd

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

//go:embed templates
var templates embed.FS

const templateDir = "templates"

// TemplateData holds the dynamic values for template processing
type TemplateData struct {
	// Git repository configuration
	GitRepoURL string
	GitBranch  string
	GitPath    string // Path within the repository (e.g., "clusters/test1")

	// Domain configuration
	Domain string

	// Provider configuration
	Provider     string // "aws", "gcp", "azure", "local"
	StorageClass string // Provider-appropriate storage class for persistent volumes

	// Certificate configuration
	CertificateIssuer string // "selfsigned-issuer" or "letsencrypt-issuer"
	ACMEEmail         string
	ACMEServer        string

	// MetalLB configuration (for local provider)
	MetalLBAddressRange string
}

// NewTemplateData creates TemplateData from NebariConfig
func NewTemplateData(cfg *config.NebariConfig) TemplateData {
	data := TemplateData{
		Domain:              cfg.Domain,
		Provider:            cfg.Provider,
		StorageClass:        storageClassForProvider(cfg.Provider),
		MetalLBAddressRange: "192.168.1.100-192.168.1.110", // Default, can be overridden
	}

	// Set git repository info
	if cfg.GitRepository != nil {
		data.GitRepoURL = cfg.GitRepository.URL
		data.GitBranch = cfg.GitRepository.GetBranch()
		data.GitPath = cfg.GitRepository.Path
	}

	// Set certificate configuration
	if cfg.Certificate != nil {
		if cfg.Certificate.Type == "letsencrypt" {
			data.CertificateIssuer = "letsencrypt-issuer"
			if cfg.Certificate.ACME != nil {
				data.ACMEEmail = cfg.Certificate.ACME.Email
				data.ACMEServer = cfg.Certificate.ACME.Server
				if data.ACMEServer == "" {
					data.ACMEServer = "https://acme-v02.api.letsencrypt.org/directory"
				}
			}
		} else {
			data.CertificateIssuer = "selfsigned-issuer"
		}
	} else {
		data.CertificateIssuer = "selfsigned-issuer"
	}

	// Default domain if not set
	if data.Domain == "" {
		data.Domain = "nebari.local"
	}

	return data
}

// Applications returns the list of available application names from the apps/ directory.
// Names are derived from filenames (without .yaml extension).
func Applications() ([]string, error) {
	entries, err := fs.ReadDir(templates, filepath.Join(templateDir, "apps"))
	if err != nil {
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var apps []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		// Skip files starting with underscore (used for examples/documentation)
		if strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		apps = append(apps, name)
	}
	return apps, nil
}

// WriteApplication writes the ArgoCD Application manifest for appName to out.
// Returns an error if the application template is not found.
func WriteApplication(ctx context.Context, out io.Writer, appName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.WriteApplication")
	defer span.End()

	span.SetAttributes(attribute.String("application", appName))

	filename := filepath.Join(templateDir, "apps", appName+".yaml")
	content, err := templates.ReadFile(filename)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("template not found for application %q: %w", appName, err)
	}

	_, err = out.Write(content)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write application %q: %w", appName, err)
	}

	return nil
}

// WriteAll writes all application manifests by calling fn for each application.
// The fn callback receives the application name and should return an io.WriteCloser
// for that application's manifest. WriteAll will close the writer after writing.
//
// Example:
//
//	err := argocd.WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
//	    return os.Create(filepath.Join(dir, appName+".yaml"))
//	})
func WriteAll(ctx context.Context, fn func(appName string) (io.WriteCloser, error)) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.WriteAll")
	defer span.End()

	apps, err := Applications()
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(attribute.Int("application_count", len(apps)))

	for _, appName := range apps {
		w, err := fn(appName)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create writer for %q: %w", appName, err)
		}

		err = WriteApplication(ctx, w, appName)
		closeErr := w.Close()

		if err != nil {
			return err
		}
		if closeErr != nil {
			span.RecordError(closeErr)
			return fmt.Errorf("failed to close writer for %q: %w", appName, closeErr)
		}
	}

	return nil
}

// WriteAllToGit writes all templates (apps and manifests) to the git repository.
// Templates are processed with Go template syntax for dynamic values.
func WriteAllToGit(ctx context.Context, gitClient git.Client, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.WriteAllToGit")
	defer span.End()

	workDir := gitClient.WorkDir()
	data := NewTemplateData(cfg)

	span.SetAttributes(
		attribute.String("work_dir", workDir),
		attribute.String("domain", data.Domain),
		attribute.String("git_repo_url", data.GitRepoURL),
	)

	// Walk all files in the templates directory
	err := fs.WalkDir(templates, templateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root templates directory
		if path == templateDir {
			return nil
		}

		// Get the relative path from templates/
		relPath := strings.TrimPrefix(path, templateDir+"/")

		// Skip hidden files and underscore-prefixed files
		if strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Skip MetalLB templates for cloud providers that use their own load balancers
		if isMetalLBPath(relPath) && !needsMetalLB(data.Provider) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(workDir, relPath)

		if d.IsDir() {
			// Create directory
			return os.MkdirAll(destPath, 0750)
		}

		// Read template content
		content, err := templates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Process template
		processed, err := processTemplate(relPath, content, data)
		if err != nil {
			return fmt.Errorf("failed to process template %s: %w", path, err)
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", destPath, err)
		}

		// Write processed content
		if err := os.WriteFile(destPath, processed, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}

		return nil
	})

	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write templates to git: %w", err)
	}

	return nil
}

// storageClassForProvider returns the appropriate storage class for the given provider.
func storageClassForProvider(provider string) string {
	switch provider {
	case "aws":
		return "gp2"
	case "gcp":
		return "standard-rwo"
	case "azure":
		return "managed-csi"
	case "local":
		return "standard"
	default:
		return "standard"
	}
}

// isMetalLBPath returns true if the relative path is a MetalLB-related template.
func isMetalLBPath(relPath string) bool {
	return relPath == "apps/metallb.yaml" ||
		relPath == "apps/metallb-config.yaml" ||
		strings.HasPrefix(relPath, "manifests/metallb")
}

// needsMetalLB returns true if the provider requires MetalLB for load balancing.
// Cloud providers (aws, gcp, azure) have native load balancers and don't need MetalLB.
func needsMetalLB(provider string) bool {
	return provider == "local"
}

// processTemplate processes a template file with the given data.
// Only YAML files are processed as templates; other files are returned as-is.
func processTemplate(name string, content []byte, data TemplateData) ([]byte, error) {
	// Only process YAML files as templates
	if !strings.HasSuffix(name, ".yaml") {
		return content, nil
	}

	tmpl, err := template.New(name).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}
