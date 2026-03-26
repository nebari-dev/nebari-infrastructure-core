package hetzner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDownloadBinary(t *testing.T) {
	fakeBinary := []byte("#!/bin/sh\necho fake-hetzner-k3s")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fakeBinary)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	downloader := &hetznerK3sDownloader{
		version:  DefaultHetznerK3sVersion,
		baseURL:  server.URL,
		cacheDir: cacheDir,
	}

	execPath, err := ensureBinary(context.Background(), cacheDir, DefaultHetznerK3sVersion, downloader)
	if err != nil {
		t.Fatalf("ensureBinary() error = %v", err)
	}

	// Verify binary was written
	content, err := os.ReadFile(execPath) //nolint:gosec // Test file, path from t.TempDir()
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}
	if string(content) != string(fakeBinary) {
		t.Error("binary content mismatch")
	}

	// Verify executable permissions
	info, _ := os.Stat(execPath)
	if info.Mode().Perm()&0111 == 0 {
		t.Error("binary should be executable")
	}
}

func TestDownloadBinary_CachesResult(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write([]byte("binary"))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	downloader := &hetznerK3sDownloader{
		version:  DefaultHetznerK3sVersion,
		baseURL:  server.URL,
		cacheDir: cacheDir,
	}

	// First call downloads
	_, err := ensureBinary(context.Background(), cacheDir, DefaultHetznerK3sVersion, downloader)
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 download, got %d", callCount)
	}

	// Second call should use cache
	_, err = ensureBinary(context.Background(), cacheDir, DefaultHetznerK3sVersion, downloader)
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected no additional download, got %d total", callCount)
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("test binary content")
	hash := sha256.Sum256(data)
	goodChecksum := hex.EncodeToString(hash[:])

	tests := []struct {
		name    string
		data    []byte
		osName  string
		arch    string
		setup   func()
		cleanup func()
		wantErr bool
		errMsg  string
	}{
		{
			name:   "matching checksum passes",
			data:   data,
			osName: "test-os",
			arch:   "test-arch",
			setup: func() {
				knownChecksums["test-os-test-arch"] = goodChecksum
			},
			cleanup: func() {
				delete(knownChecksums, "test-os-test-arch")
			},
		},
		{
			name:   "mismatched checksum fails",
			data:   []byte("wrong content"),
			osName: "test-os",
			arch:   "test-arch",
			setup: func() {
				knownChecksums["test-os-test-arch"] = goodChecksum
			},
			cleanup: func() {
				delete(knownChecksums, "test-os-test-arch")
			},
			wantErr: true,
			errMsg:  "checksum mismatch",
		},
		{
			name:   "unknown platform skips verification",
			data:   []byte("anything"),
			osName: "unknown-os",
			arch:   "unknown-arch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			if tt.cleanup != nil {
				defer tt.cleanup()
			}
			err := verifyChecksum(tt.data, tt.osName, tt.arch)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyChecksum() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
			}
		})
	}
}
