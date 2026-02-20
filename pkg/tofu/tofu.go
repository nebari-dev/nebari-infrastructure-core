package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/opentofu/tofudl"
	"github.com/spf13/afero"
)

// Conservative timeout for network download
const downloadTimeout = 10 * time.Minute

// TerraformExecutor wraps a Terraform executor with its working directory for cleanup.
type TerraformExecutor struct {
	*tfexec.Terraform
	workingDir string
	appFs      afero.Fs
}

// Cleanup removes the temporary working directory.
func (te *TerraformExecutor) Cleanup() error {
	return te.appFs.RemoveAll(te.workingDir)
}

// Init wraps tfexec.Terraform.Init with a signal-safe context.
func (te *TerraformExecutor) Init(ctx context.Context, opts ...tfexec.InitOption) error {
	return te.Terraform.Init(signalSafeContext(ctx), opts...)
}

// Plan wraps tfexec.Terraform.Plan with a signal-safe context.
func (te *TerraformExecutor) Plan(ctx context.Context, opts ...tfexec.PlanOption) (bool, error) {
	return te.Terraform.Plan(signalSafeContext(ctx), opts...)
}

// Apply wraps tfexec.Terraform.Apply with a signal-safe context.
func (te *TerraformExecutor) Apply(ctx context.Context, opts ...tfexec.ApplyOption) error {
	return te.Terraform.Apply(signalSafeContext(ctx), opts...)
}

// Destroy wraps tfexec.Terraform.Destroy with a signal-safe context.
func (te *TerraformExecutor) Destroy(ctx context.Context, opts ...tfexec.DestroyOption) error {
	return te.Terraform.Destroy(signalSafeContext(ctx), opts...)
}

// Output wraps tfexec.Terraform.Output with a signal-safe context.
func (te *TerraformExecutor) Output(ctx context.Context) (map[string]tfexec.OutputMeta, error) {
	return te.Terraform.Output(signalSafeContext(ctx))
}

// backendOverrideJSON overrides the configured backend with a local backend.
const backendOverrideJSON = `{
  "terraform": {
    "backend": {
      "local": {}
    }
  }
}
`

// WriteBackendOverride writes a backend_override.tf.json file to the working directory
// that overrides the configured backend with a local backend. This is useful for
// dry-run scenarios where the remote state bucket does not yet exist.
func (te *TerraformExecutor) WriteBackendOverride() error {
	path := filepath.Join(te.workingDir, "backend_override.tf.json")
	if err := afero.WriteFile(te.appFs, path, []byte(backendOverrideJSON), 0644); err != nil {
		return fmt.Errorf("failed to write backend override: %w", err)
	}

	return nil
}

// binaryDownloader abstracts binary fetching to enable testing.
// Tests can provide a mock implementation that returns fake binary data,
// allowing downloadExecutable to be tested without network access.
type binaryDownloader interface {
	download(ctx context.Context) ([]byte, error)
}

// tofuDownloader implements binaryDownloader using tofudl with caching.
type tofuDownloader struct {
	cacheDir string
	version  string
}

// download fetches the OpenTofu binary for the current platform.
func (d *tofuDownloader) download(ctx context.Context) ([]byte, error) {
	// Make sure to have a context with timeout so downloads don't hang indefinitely.
	// If caller sets a shorter timeout, theirs takes precedence.
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	dl, err := tofudl.New()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tofu downloader: %w", err)
	}

	storage, err := tofudl.NewFilesystemStorage(d.cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tofu filesystem storage: %w", err)
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
		return nil, fmt.Errorf("failed to initialize tofu mirror: %w", err)
	}

	ver := tofudl.Version(d.version)
	opts := tofudl.DownloadOptVersion(ver)
	binary, err := mirror.Download(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to download tofu binary: %w", err)
	}

	return binary, nil
}

func getCacheDir(appFs afero.Fs) (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user cache directory: %w", err)
	}

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

// downloadExecutable writes the binary from downloader to the cache directory.
// It's separate from download to allow testing the file-writing logic independently
// by injecting a mock binaryDownloader that doesn't require network access.
func downloadExecutable(ctx context.Context, appFs afero.Fs, cacheDir string, downloader binaryDownloader) (string, error) {
	binary, err := downloader.download(ctx)
	if err != nil {
		return "", err
	}

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

		if path == templatesDir {
			return nil
		}

		relPath, err := filepath.Rel(templatesDir, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dir, relPath)

		if d.IsDir() {
			return appFs.MkdirAll(targetPath, 0755)
		}

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
// and writing tfvars. Returns a TerraformExecutor configured with stdout/stderr.
// The caller is responsible for calling Init() and Apply() with appropriate options and
// deferring Cleanup() to remove the temporary working directory.
// The binary and providers are cached in ~/.cache/nic/tofu/ to avoid re-downloading on subsequent runs.
func Setup(ctx context.Context, templates fs.FS, tfvars any) (te *TerraformExecutor, err error) {
	appFs := afero.NewOsFs()

	cacheDir, err := getCacheDir(appFs)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}

	pluginCacheDir, err := getPluginCacheDir(appFs, cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin cache directory: %w", err)
	}

	downloader := &tofuDownloader{cacheDir: cacheDir, version: Version}
	execPath, err := downloadExecutable(ctx, appFs, cacheDir, downloader)
	if err != nil {
		return nil, fmt.Errorf("failed to get executable: %w", err)
	}

	workingDir, err := extractTemplates(appFs, templates)
	if err != nil {
		return nil, err
	}

	// Remove workingDir and its contents if we return an error after this point
	defer func() {
		if err != nil {
			_ = appFs.RemoveAll(workingDir)
		}
	}()

	if err = os.Setenv("TF_PLUGIN_CACHE_DIR", pluginCacheDir); err != nil {
		return nil, fmt.Errorf("failed to set TF_PLUGIN_CACHE_DIR: %w", err)
	}

	tfvarsJSON, err := json.Marshal(tfvars)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tfvars: %w", err)
	}
	if err = afero.WriteFile(appFs, filepath.Join(workingDir, "terraform.tfvars.json"), tfvarsJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write tfvars: %w", err)
	}

	tf, err := tfexec.NewTerraform(workingDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform executor: %w", err)
	}

	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	return &TerraformExecutor{
		Terraform:  tf,
		workingDir: workingDir,
		appFs:      appFs,
	}, nil
}
