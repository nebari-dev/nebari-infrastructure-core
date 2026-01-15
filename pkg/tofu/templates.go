package tofu

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed templates
var tofuTemplates embed.FS

func ExtractTemplates(providerName string) (string, error) {
	dir, err := os.MkdirTemp("", "tofu")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Path to provider-specific templates
	providerPath := filepath.Join("templates", providerName)

	err = fs.WalkDir(tofuTemplates, providerPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == providerPath {
			return nil
		}

		// Calculate relative path (remove "templates/providerName/" prefix)
		relPath, err := filepath.Rel(providerPath, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// Read file from embedded FS
		data, err := fs.ReadFile(tofuTemplates, path)
		if err != nil {
			return err
		}

		// Write to temp directory
		return os.WriteFile(targetPath, data, 0644)
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("failed to extract templates for %s: %w", providerName, err)
	}

	return dir, nil
}
