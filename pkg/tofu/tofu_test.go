package tofu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
)

// mockDownloader implements binaryDownloader for testing.
type mockDownloader struct {
	binary []byte
	err    error
}

func (m *mockDownloader) download(ctx context.Context) ([]byte, error) {
	return m.binary, m.err
}

func TestGetCacheDir(t *testing.T) {
	t.Run("creates directory if not exists", func(t *testing.T) {
		memFs := afero.NewMemMapFs()

		cacheDir, err := getCacheDir(memFs)
		if err != nil {
			t.Fatalf("getCacheDir() error = %v", err)
		}

		if !strings.HasSuffix(cacheDir, filepath.Join("nic", "tofu")) {
			t.Errorf("getCacheDir() = %v, want path ending with nic/tofu", cacheDir)
		}

		exists, err := afero.DirExists(memFs, cacheDir)
		if err != nil {
			t.Fatalf("Failed to check directory: %v", err)
		}
		if !exists {
			t.Errorf("getCacheDir() did not create directory")
		}
	})

	t.Run("succeeds if directory already exists", func(t *testing.T) {
		memFs := afero.NewMemMapFs()

		userCache, _ := os.UserCacheDir()
		existingDir := filepath.Join(userCache, "nic", "tofu")
		if err := memFs.MkdirAll(existingDir, 0755); err != nil {
			t.Fatalf("Failed to pre-create directory: %v", err)
		}

		cacheDir, err := getCacheDir(memFs)
		if err != nil {
			t.Fatalf("getCacheDir() error = %v", err)
		}

		if cacheDir != existingDir {
			t.Errorf("getCacheDir() = %v, want %v", cacheDir, existingDir)
		}
	})
}

func TestGetPluginCacheDir(t *testing.T) {
	t.Run("creates plugins subdirectory", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		baseDir, err := afero.TempDir(memFs, "", "tofu-cache")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}

		pluginDir, err := getPluginCacheDir(memFs, baseDir)
		if err != nil {
			t.Fatalf("getPluginCacheDir() error = %v", err)
		}

		expected := filepath.Join(baseDir, "plugins")
		if pluginDir != expected {
			t.Errorf("getPluginCacheDir() = %v, want %v", pluginDir, expected)
		}

		exists, err := afero.DirExists(memFs, pluginDir)
		if err != nil {
			t.Fatalf("Failed to check directory: %v", err)
		}
		if !exists {
			t.Errorf("getPluginCacheDir() did not create directory")
		}
	})

	t.Run("succeeds if directory already exists", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		baseDir, err := afero.TempDir(memFs, "", "tofu-cache")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		existingDir := filepath.Join(baseDir, "plugins")

		if err := memFs.MkdirAll(existingDir, 0755); err != nil {
			t.Fatalf("Failed to pre-create directory: %v", err)
		}

		pluginDir, err := getPluginCacheDir(memFs, baseDir)
		if err != nil {
			t.Fatalf("getPluginCacheDir() error = %v", err)
		}

		if pluginDir != existingDir {
			t.Errorf("getPluginCacheDir() = %v, want %v", pluginDir, existingDir)
		}
	})
}

// TestExtractTemplates uses fstest.MapFS to simulate embed.FS behavior.
// In the app, templates are embedded via //go:embed directive, which creates
// an embed.FS with files under a "templates/" prefix. MapFS lets us create
// the same structure in-memory without needing actual files on disk.
func TestExtractTemplates(t *testing.T) {
	t.Run("extracts single file", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		templateFs := fstest.MapFS{
			"templates/main.tf": &fstest.MapFile{Data: []byte("# test template")},
		}

		dir, err := extractTemplates(memFs, templateFs)
		if err != nil {
			t.Fatalf("extractTemplates() error = %v", err)
		}

		content, err := afero.ReadFile(memFs, filepath.Join(dir, "main.tf"))
		if err != nil {
			t.Fatalf("Failed to read extracted file: %v", err)
		}

		if string(content) != "# test template" {
			t.Errorf("content = %q, want %q", string(content), "# test template")
		}
	})

	t.Run("extracts multiple files", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		templateFs := fstest.MapFS{
			"templates/main.tf":      &fstest.MapFile{Data: []byte("# main")},
			"templates/variables.tf": &fstest.MapFile{Data: []byte("# variables")},
			"templates/outputs.tf":   &fstest.MapFile{Data: []byte("# outputs")},
		}

		dir, err := extractTemplates(memFs, templateFs)
		if err != nil {
			t.Fatalf("extractTemplates() error = %v", err)
		}

		files := []string{"main.tf", "variables.tf", "outputs.tf"}
		for _, f := range files {
			exists, err := afero.Exists(memFs, filepath.Join(dir, f))
			if err != nil {
				t.Fatalf("Failed to check file %s: %v", f, err)
			}
			if !exists {
				t.Errorf("File %s was not extracted", f)
			}
		}
	})

	t.Run("extracts nested directories", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		templateFs := fstest.MapFS{
			"templates/main.tf":             &fstest.MapFile{Data: []byte("# root")},
			"templates/modules/vpc/main.tf": &fstest.MapFile{Data: []byte("# vpc module")},
		}

		dir, err := extractTemplates(memFs, templateFs)
		if err != nil {
			t.Fatalf("extractTemplates() error = %v", err)
		}

		content, err := afero.ReadFile(memFs, filepath.Join(dir, "modules", "vpc", "main.tf"))
		if err != nil {
			t.Fatalf("Failed to read nested file: %v", err)
		}

		if string(content) != "# vpc module" {
			t.Errorf("nested content = %q, want %q", string(content), "# vpc module")
		}
	})

	t.Run("creates temp directory with correct prefix", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		templateFs := fstest.MapFS{
			"templates/main.tf": &fstest.MapFile{Data: []byte("# test")},
		}

		dir, err := extractTemplates(memFs, templateFs)
		if err != nil {
			t.Fatalf("extractTemplates() error = %v", err)
		}

		if !strings.Contains(dir, "nic-tofu") {
			t.Errorf("dir = %v, want path containing 'nic-tofu'", dir)
		}
	})

	t.Run("extracts dotfiles", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		templateFs := fstest.MapFS{
			"templates/.terraform.lock.hcl": &fstest.MapFile{Data: []byte("# lock file")},
			"templates/main.tf":             &fstest.MapFile{Data: []byte("# main")},
		}

		dir, err := extractTemplates(memFs, templateFs)
		if err != nil {
			t.Fatalf("extractTemplates() error = %v", err)
		}

		content, err := afero.ReadFile(memFs, filepath.Join(dir, ".terraform.lock.hcl"))
		if err != nil {
			t.Fatalf("Failed to read dotfile: %v", err)
		}

		if string(content) != "# lock file" {
			t.Errorf("dotfile content = %q, want %q", string(content), "# lock file")
		}
	})
}

func TestWriteBackendOverride(t *testing.T) {
	t.Run("writes backend override file with correct content", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		workingDir, err := afero.TempDir(memFs, "", "nic-tofu")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}

		te := &TerraformExecutor{
			workingDir: workingDir,
			appFs:      memFs,
		}

		if err := te.WriteBackendOverride(); err != nil {
			t.Fatalf("WriteBackendOverride() error = %v", err)
		}

		content, err := afero.ReadFile(memFs, filepath.Join(workingDir, "backend_override.tf.json"))
		if err != nil {
			t.Fatalf("Failed to read override file: %v", err)
		}

		if string(content) != backendOverrideJSON {
			t.Errorf("content = %q, want %q", string(content), backendOverrideJSON)
		}
	})
}

func TestDownloadExecutable(t *testing.T) {
	t.Run("writes binary to directory", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		dir, err := afero.TempDir(memFs, "", "tofu-working")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}

		fakeBinary := []byte("fake tofu binary content")
		downloader := &mockDownloader{binary: fakeBinary}

		execPath, err := downloadExecutable(context.Background(), memFs, dir, downloader)
		if err != nil {
			t.Fatalf("downloadExecutable() error = %v", err)
		}

		// Verify the path is correct
		expectedName := "tofu"
		if runtime.GOOS == "windows" {
			expectedName = "tofu.exe"
		}
		if filepath.Base(execPath) != expectedName {
			t.Errorf("execPath = %v, want filename %v", execPath, expectedName)
		}

		// Verify binary was written
		content, err := afero.ReadFile(memFs, execPath)
		if err != nil {
			t.Fatalf("Failed to read binary: %v", err)
		}
		if string(content) != string(fakeBinary) {
			t.Errorf("binary content mismatch")
		}
	})

	t.Run("returns error when download fails", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		dir, err := afero.TempDir(memFs, "", "tofu-working")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}

		downloader := &mockDownloader{err: errors.New("network error")}

		_, err = downloadExecutable(context.Background(), memFs, dir, downloader)
		if err == nil {
			t.Fatal("downloadExecutable() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "network error") {
			t.Errorf("error = %v, want error containing 'network error'", err)
		}
	})
}
