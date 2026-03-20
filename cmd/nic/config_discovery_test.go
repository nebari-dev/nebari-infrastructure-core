package main

import (
	"os"
	"testing"
)

// writeTempConfig creates a readable config.yaml in dir and returns its path.
func writeTempConfig(t *testing.T, dir string) string {
	t.Helper()
	path := dir + "/" + defaultConfigFilename
	if err := os.WriteFile(path, []byte("provider: local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveConfigFile_ExplicitFlag(t *testing.T) {
	path := writeTempConfig(t, t.TempDir())

	got, err := resolveConfigFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestResolveConfigFile_ExplicitFlag_Unreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
	path := writeTempConfig(t, t.TempDir())
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}

	_, err := resolveConfigFile(path)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
}

func TestResolveConfigFile_EnvVar(t *testing.T) {
	path := writeTempConfig(t, t.TempDir())
	t.Setenv(envConfigPath, path)

	got, err := resolveConfigFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestResolveConfigFile_EnvVar_Unreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
	path := writeTempConfig(t, t.TempDir())
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envConfigPath, path)

	_, err := resolveConfigFile("")
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
}

func TestResolveConfigFile_ExplicitFlagTakesPriorityOverEnv(t *testing.T) {
	explicit := writeTempConfig(t, t.TempDir())
	env := writeTempConfig(t, t.TempDir())
	t.Setenv(envConfigPath, env)

	got, err := resolveConfigFile(explicit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != explicit {
		t.Errorf("got %q, want %q", got, explicit)
	}
}

func TestResolveConfigFile_AutoDiscoverCurrentDir(t *testing.T) {
	dir := t.TempDir()
	writeTempConfig(t, dir)

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	t.Setenv(envConfigPath, "")

	got, err := resolveConfigFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultConfigFilename {
		t.Errorf("got %q, want %q", got, defaultConfigFilename)
	}
}

func TestResolveConfigFile_AutoDiscover_Unreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
	dir := t.TempDir()
	path := writeTempConfig(t, dir)
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	t.Setenv(envConfigPath, "")

	_, err = resolveConfigFile("")
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
}

func TestResolveConfigFile_NothingFound(t *testing.T) {
	t.Setenv(envConfigPath, "")

	// Change to an empty temporary directory so no local config.yaml exists.
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, err = resolveConfigFile("")
	if err == nil {
		t.Fatal("expected error when no config file is found, got nil")
	}
}

func TestFileExists_ExistingFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if !fileExists(f.Name()) {
		t.Errorf("fileExists(%q) = false, want true", f.Name())
	}
}

func TestFileExists_Directory(t *testing.T) {
	dir := t.TempDir()
	if fileExists(dir) {
		t.Errorf("fileExists(%q) = true for directory, want false", dir)
	}
}

func TestFileExists_Missing(t *testing.T) {
	if fileExists("/nonexistent/path/config.yaml") {
		t.Error("fileExists() = true for nonexistent path, want false")
	}
}
