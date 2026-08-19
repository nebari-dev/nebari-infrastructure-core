package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
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
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

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

// TestAnnotateConfigError verifies that placeholder errors are enriched with the
// config file path (so the user sees both the field and the file), while other
// errors pass through unchanged. The error is passed bare, which is the shape
// rejectPlaceholders returns for a placeholder: the check no longer runs inside
// NebariConfig.Validate, so no "configuration validation failed" wrapper sits
// between the two.
func TestAnnotateConfigError(t *testing.T) {
	placeholderErr := error(&config.PlaceholderError{FieldPaths: []string{"cluster.aws.region"}})
	got := annotateConfigError(placeholderErr, "/path/to/nebari-config.yaml")
	if !strings.Contains(got.Error(), "cluster.aws.region") {
		t.Errorf("annotated error %q does not mention the field path", got)
	}
	if !strings.Contains(got.Error(), "/path/to/nebari-config.yaml") {
		t.Errorf("annotated error %q does not mention the config file", got)
	}
	var pErr *config.PlaceholderError
	if !errors.As(got, &pErr) {
		t.Errorf("annotated error no longer unwraps to *PlaceholderError")
	}

	other := errors.New("some other validation failure")
	if got := annotateConfigError(other, "/path/to/nebari-config.yaml"); got != other {
		t.Errorf("annotateConfigError modified a non-placeholder error: %v", got)
	}
}

// writeTempConfigFile writes raw to a nebari-config.yaml inside dir and returns
// the path.
func writeTempConfigFile(t *testing.T, dir, raw string) string {
	t.Helper()
	path := filepath.Join(dir, "nebari-config.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// newTestCommand returns a cobra command carrying a context, as the RunE
// functions expect from cobra's Execute.
func newTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

// assertPlaceholderRejected checks that err is the annotated placeholder error a
// command must return: it unwraps to *config.PlaceholderError, names every
// offending field, and names the config file to edit.
func assertPlaceholderRejected(t *testing.T, err error, configFile string, wantFields ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("command returned nil, want a placeholder error")
	}
	var placeholderErr *config.PlaceholderError
	if !errors.As(err, &placeholderErr) {
		t.Fatalf("error %v does not unwrap to *config.PlaceholderError", err)
	}
	for _, field := range wantFields {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not mention field %q", err, field)
		}
	}
	if !strings.Contains(err.Error(), configFile) {
		t.Errorf("error %q does not mention the config file %q", err, configFile)
	}
}

// TestRunValidate_RejectsPlaceholders exercises the validate command end to end
// against an unedited config. It is the red step for the WIRING: deleting the
// rejectPlaceholders call in validate.go must fail this test, which scanner-level
// tests in pkg/config cannot catch. The command returns before nic.NewClient, so
// no provider or network access is involved.
func TestRunValidate_RejectsPlaceholders(t *testing.T) {
	configFile := writeTempConfigFile(t, t.TempDir(), `project_name: CHANGEME
domain: nebari.example.com
cluster:
  aws:
    region: CHANGEME
`)

	validateConfigFile = configFile
	t.Cleanup(func() { validateConfigFile = "" })

	assertPlaceholderRejected(t, runValidate(newTestCommand(t), nil), configFile,
		"cluster.aws.region", "project_name")
}

// TestRunDeploy_RejectsPlaceholders is the same red step for the deploy command,
// which wires rejectPlaceholders independently of validate.
//
// The fixture deliberately uses the `existing` cluster provider rather than a
// cloud one. If the gate ever regresses, runDeploy falls through to
// client.Deploy, and pkg/nic.Deploy calls the provider's Deploy without a
// preceding provider-level Validate: an aws fixture would then reach STS and
// ensureStateBucket, i.e. a regression in this test would CREATE cloud
// resources. existing.Deploy is a no-op against an unreachable kubeconfig, so
// the failure stays local. kubeconfig is pinned to a nonexistent path on
// purpose - an empty value falls back to $KUBECONFIG / ~/.kube/config, which
// would aim a regressed test at the developer's own cluster. dry-run is set for
// the same reason, to keep any future regression off the apply path.
func TestRunDeploy_RejectsPlaceholders(t *testing.T) {
	dir := t.TempDir()
	configFile := writeTempConfigFile(t, dir, `project_name: CHANGEME
domain: nebari.example.com
cluster:
  existing:
    kubeconfig: `+filepath.Join(dir, "nonexistent-kubeconfig")+`
    context: CHANGEME
`)

	deployConfigFile = configFile
	deployDryRun = true
	t.Cleanup(func() {
		deployConfigFile = ""
		deployDryRun = false
	})

	assertPlaceholderRejected(t, runDeploy(newTestCommand(t), nil), configFile,
		"cluster.existing.context", "project_name")
}
