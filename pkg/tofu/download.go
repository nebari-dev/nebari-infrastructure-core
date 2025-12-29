package tofu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/opentofu/tofudl"
)

// Download downloads the OpenTofu binary for the specified version using a caching 
// strategy and returns the path to the cached binary.
func Download(ctx context.Context) (string, error) {

	// Create nic's cache directory if it doesn't exist
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user cache directory: %w", err)
	}

	nicCacheDir := filepath.Join(userCacheDir, "nic")
	if err := os.MkdirAll(nicCacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create nic cache directory: %w", err)
	}

	// Initialize tofu downloader
	dl, err := tofudl.New()
	if err != nil {
		return "", fmt.Errorf("failed to initialize tofu downloader: %w", err)
	}

	// Setup caching layer for tofu binaries
	storage, err := tofudl.NewFilesystemStorage(nicCacheDir)
	if err != nil {
		return "", fmt.Errorf("failed to initialize tofu filesystem storage: %w", err)
	}
	mirror, err := tofudl.NewMirror(
		tofudl.MirrorConfig{
			AllowStale:           true, // Use cached binary if download fails
			APICacheTimeout:      -1,   // Cache API responses indefinitely
			ArtifactCacheTimeout: -1,   // Cache binaries indefinitely
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
	execPath := filepath.Join(nicCacheDir, "tofu")
	if runtime.GOOS == "windows" {
		execPath += ".exe"
	}
	if err := os.WriteFile(execPath, binary, 0755); err != nil {
		return "", fmt.Errorf("failed to write tofu binary to cache: %w", err)
	}

	return execPath, nil
}
