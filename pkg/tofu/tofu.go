package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/opentofu/tofudl"
	"github.com/spf13/afero"
)

func getCacheDir(appFs afero.Fs) (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user cache directory: %w", err)
	}

	// Create nic/tofu cache directory if it doesn't exist
	tofuCacheDir := filepath.Join(userCacheDir, "nic", "tofu")
	if err := appFs.MkdirAll(tofuCacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create nic/tofu cache directory: %w", err)
	}

	return tofuCacheDir, nil
}

func getPluginCacheDir(appFs afero.Fs, cacheDir string) (string, error) {
	pluginCacheDir := filepath.Join(cacheDir, "plugins")
	if err := appFs.MkdirAll(pluginCacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create plugin cache directory: %w", err)
	}

	return pluginCacheDir, nil
}

func downloadExecutable(ctx context.Context, appFs afero.Fs, cacheDir string) (string, error) {
	// Initialize tofu downloader
	dl, err := tofudl.New()
	if err != nil {
		return "", fmt.Errorf("failed to initialize tofu downloader: %w", err)
	}

	// Setup caching layer for tofu binaries
	storage, err := tofudl.NewFilesystemStorage(cacheDir)
	if err != nil {
		return "", fmt.Errorf("failed to initialize tofu filesystem storage: %w", err)
	}
	mirror, err := tofudl.NewMirror(
		tofudl.MirrorConfig{
			APICacheTimeout:      -1, // Cache API responses indefinitely
			ArtifactCacheTimeout: -1, // Cache binaries indefinitely
		},
		storage,
		dl,
	)
	if err != nil {
		return "", fmt.Errorf("failed to initialize tofu mirror: %w", err)
	}

	// Download specific version for the current architecture and platform
	ver := tofudl.Version(DefaultVersion)
	opts := tofudl.DownloadOptVersion(ver)
	binary, err := mirror.Download(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("failed to download tofu binary: %w", err)
	}

	// Write binary to cache directory
	execPath := filepath.Join(cacheDir, "tofu")
	if runtime.GOOS == "windows" {
		execPath += ".exe"
	}
	if err := afero.WriteFile(appFs, execPath, binary, 0755); err != nil {
		return "", fmt.Errorf("failed to write tofu binary to cache: %w", err)
	}

	return execPath, nil
}

func extractTemplates(appFs afero.Fs, templates fs.FS) (string, error) {
	dir, err := afero.TempDir(appFs, "", "nic-tofu")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	const templatesDir = "templates"

	err = fs.WalkDir(templates, templatesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == templatesDir {
			return nil
		}

		// Calculate relative path (remove "templates/" prefix)
		relPath, err := filepath.Rel(templatesDir, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dir, relPath)

		// Create directories as needed
		if d.IsDir() {
			return appFs.MkdirAll(targetPath, 0755)
		}

		// Read file from embedded FS
		data, err := fs.ReadFile(templates, path)
		if err != nil {
			return err
		}

		return afero.WriteFile(appFs, targetPath, data, 0644)
	})
	if err != nil {
		_ = appFs.RemoveAll(dir)
		return "", fmt.Errorf("failed to extract templates: %w", err)
	}

	return dir, nil
}

// Setup prepares the OpenTofu environment by downloading the binary (if not cached),
// configuring provider plugin caching, extracting provider-specific templates,
// and writing tfvars. Returns a Terraform executor configured with stdout/stderr.
// The caller is responsible for calling Init() with appropriate options.
// The binary and providers are cached in ~/.cache/nic/tofu/ to avoid re-downloading on subsequent runs.
func Setup(ctx context.Context, templates fs.FS, tfvars any) (*tfexec.Terraform, error) {
	appFs := afero.NewOsFs()

	// Compute cache directories once
	cacheDir, err := getCacheDir(appFs)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}

	pluginCacheDir, err := getPluginCacheDir(appFs, cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin cache directory: %w", err)
	}

	execPath, err := downloadExecutable(ctx, appFs, cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get executable: %w", err)
	}

	workingDir, err := extractTemplates(appFs, templates)
	if err != nil {
		return nil, err
	}

	// Set plugin cache environment variable for Terraform
	if err := os.Setenv("TF_PLUGIN_CACHE_DIR", pluginCacheDir); err != nil {
		return nil, fmt.Errorf("failed to set TF_PLUGIN_CACHE_DIR: %w", err)
	}

	// Write tfvars to working directory
	tfvarsJSON, err := json.Marshal(tfvars)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tfvars: %w", err)
	}
	if err := afero.WriteFile(appFs, filepath.Join(workingDir, "terraform.tfvars.json"), tfvarsJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write tfvars: %w", err)
	}

	tf, err := tfexec.NewTerraform(workingDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform executor: %w", err)
	}

	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	return tf, nil
}
