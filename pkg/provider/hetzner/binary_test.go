package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
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
