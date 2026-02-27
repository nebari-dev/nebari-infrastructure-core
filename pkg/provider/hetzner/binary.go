package hetzner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	// DefaultHetznerK3sVersion is the pinned hetzner-k3s release version.
	DefaultHetznerK3sVersion = "v2.4.5"

	defaultBaseURL  = "https://github.com/vitobotta/hetzner-k3s/releases/download"
	downloadTimeout = 5 * time.Minute
)

// binaryDownloader abstracts binary fetching for testability.
type binaryDownloader interface {
	download(ctx context.Context) ([]byte, error)
}

// hetznerK3sDownloader fetches the hetzner-k3s binary from GitHub releases.
type hetznerK3sDownloader struct {
	version  string
	baseURL  string // override for testing
	cacheDir string
}

func (d *hetznerK3sDownloader) download(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("unsupported architecture: %s", arch)
	}

	base := d.baseURL
	if base == "" {
		base = defaultBaseURL
	}

	url := fmt.Sprintf("%s/%s/hetzner-k3s-%s-%s", base, d.version, osName, arch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download hetzner-k3s binary: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Best-effort close on read-only response

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}

	return body, nil
}

// ensureBinary returns the path to the hetzner-k3s binary, downloading it if not cached.
func ensureBinary(ctx context.Context, cacheDir string, downloader binaryDownloader) (string, error) {
	execPath := filepath.Join(cacheDir, "hetzner-k3s")
	if runtime.GOOS == "windows" {
		execPath += ".exe"
	}

	if _, err := os.Stat(execPath); err == nil {
		return execPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	binary, err := downloader.download(ctx)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(execPath, binary, 0755); err != nil { //nolint:gosec // Binary must be executable
		return "", fmt.Errorf("failed to write hetzner-k3s binary: %w", err)
	}

	return execPath, nil
}

// getHetznerCacheDir returns the hetzner-k3s cache directory.
func getHetznerCacheDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user cache directory: %w", err)
	}
	return filepath.Join(userCacheDir, "nic", "hetzner-k3s"), nil
}
