package hetzner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// verifyChecksum validates a SHA256 checksum if a known digest is available.
// Returns nil if the digest matches or if no known digest exists for this platform.
func verifyChecksum(data []byte, osName, arch string) error {
	key := osName + "-" + arch
	expected, ok := knownChecksums[key]
	if !ok {
		return nil
	}
	actual := sha256.Sum256(data)
	actualHex := hex.EncodeToString(actual[:])
	if actualHex != expected {
		return fmt.Errorf("SHA256 checksum mismatch for hetzner-k3s %s-%s: expected %s, got %s", osName, arch, expected, actualHex)
	}
	return nil
}

const (
	// DefaultHetznerK3sVersion is the pinned hetzner-k3s release version.
	DefaultHetznerK3sVersion = "v2.4.5"

	defaultBaseURL  = "https://github.com/vitobotta/hetzner-k3s/releases/download"
	downloadTimeout = 5 * time.Minute

	// maxBinarySize limits the download to 100 MB to prevent OOM from rogue servers.
	maxBinarySize = 100 * 1024 * 1024

	userAgent = "nic/hetzner-provider"
)

// knownChecksums maps "os-arch" to the expected SHA256 hex digest for the pinned version.
// Update these when bumping DefaultHetznerK3sVersion.
var knownChecksums = map[string]string{
	"linux-amd64": "18bbfe3d066539a967419d052ac0f8b4ad4691f2f76f9f22d7433c10ef28fea5",
	"linux-arm64": "0b60c018842fd7f6c53116e439f5e25ec8b0c5d7d04710c81f7d50549e6fb194",
	"macos-amd64": "803b2503a9bad0f9dbeadcc8f7ab23844e1d027da6ab27dd627ccd53a6000818",
	"macos-arm64": "31d69c5666c3e4a96309ca770c80e03d846d1c83754f49679765b1588806c1bd",
}

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
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download hetzner-k3s binary: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Best-effort close on read-only response

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBinarySize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}
	if int64(len(body)) > maxBinarySize {
		return nil, fmt.Errorf("downloaded binary exceeds maximum size of %d bytes", maxBinarySize)
	}

	// Only verify checksum when downloading from the official release URL.
	// Test servers use fake binaries that won't match real checksums.
	if d.baseURL == "" {
		if err := verifyChecksum(body, osName, arch); err != nil {
			return nil, err
		}
	}

	return body, nil
}

// ensureBinary returns the path to the hetzner-k3s binary, downloading it if not cached.
// The binary is cached with its version in the filename to avoid reusing stale binaries
// after version bumps.
func ensureBinary(ctx context.Context, cacheDir, version string, downloader binaryDownloader) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.ensureBinary")
	defer span.End()

	span.SetAttributes(attribute.String("version", version))

	execPath := filepath.Join(cacheDir, fmt.Sprintf("hetzner-k3s-%s", version))
	if runtime.GOOS == "windows" {
		execPath += ".exe"
	}

	if _, err := os.Stat(execPath); err == nil {
		span.SetAttributes(attribute.Bool("cached", true))
		return execPath, nil
	}
	span.SetAttributes(attribute.Bool("cached", false))

	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	binary, err := downloader.download(ctx)
	if err != nil {
		return "", err
	}

	// Record whether the platform had a known checksum for observability.
	// verifyChecksum silently passes for unknown platforms (forward compatibility),
	// so trace consumers can detect unverified binaries.
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	_, hasChecksum := knownChecksums[osName+"-"+runtime.GOARCH]
	span.SetAttributes(attribute.Bool("checksum_verified", hasChecksum))

	// Write to a temp file first, then atomically rename to avoid TOCTOU races
	// where a concurrent process could observe a partially-written binary.
	tmpPath := execPath + ".tmp"
	if err := os.WriteFile(tmpPath, binary, 0755); err != nil { //nolint:gosec // Binary must be executable
		return "", fmt.Errorf("failed to write hetzner-k3s binary: %w", err)
	}
	// Clean up temp file on rename failure. After a successful rename the
	// original path no longer exists, so Remove is a harmless no-op.
	defer os.Remove(tmpPath) //nolint:errcheck // Best-effort cleanup
	if err := os.Rename(tmpPath, execPath); err != nil {
		return "", fmt.Errorf("failed to finalize hetzner-k3s binary: %w", err)
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
