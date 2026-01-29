// Package argocd generates ArgoCD Application manifests for Nebari's
// foundational software stack.
package argocd

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

//go:embed templates/*.yaml
var templates embed.FS

const templateDir = "templates"

// Applications returns the list of available application names.
// Names are derived from filenames in the templates directory (without .yaml extension).
func Applications() ([]string, error) {
	entries, err := fs.ReadDir(templates, templateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read templates directory: %w", err)
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

	filename := templateDir + "/" + appName + ".yaml"
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
