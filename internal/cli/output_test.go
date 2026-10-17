package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// TestCommandOutputLogFile runs a short status stream through
// startCommandOutput and reads back what reached the log file.
func TestCommandOutputLogFile(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		flags     logFileFlags
		err       error
		wantFile  bool
		wantFinal string
	}{
		{name: "default path under ~/.nic/logs", project: "acme", wantFile: true, wantFinal: "Command completed"},
		{name: "failure is recorded", project: "acme", err: errors.New("boom"), wantFile: true, wantFinal: "Command execution failed"},
		{name: "disabled", project: "acme", flags: logFileFlags{disabled: true}},
		{name: "unvalidated project name never escapes the log directory", project: "../../escaped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			outputFormat = output.FormatPlain
			t.Cleanup(func() { outputFormat = "" })

			cfg := &config.NebariConfig{ProjectName: tt.project, Cluster: &config.ClusterConfig{}}
			ctx, out := startCommandOutput(context.Background(), "deploy", cfg, output.Header{}, tt.flags, false)
			status.Phase(ctx, "Infrastructure")
			status.Success(ctx, "Infrastructure deployment completed")
			out.finish(tt.err, "")

			files, _ := filepath.Glob(filepath.Join(home, ".nic", "logs", "*", "*-deploy.jsonl"))
			escaped, _ := filepath.Glob(filepath.Join(home, "escaped", "*")) // where ../../escaped would land
			if len(escaped) > 0 {
				t.Fatalf("log file written outside ~/.nic/logs: %v", escaped)
			}
			if !tt.wantFile {
				if len(files) != 0 {
					t.Fatalf("want no log file, got %v", files)
				}
				return
			}
			if len(files) != 1 {
				t.Fatalf("want one log file, got %v", files)
			}
			info, err := os.Stat(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("log file mode = %v, want 0600", info.Mode().Perm())
			}

			var msgs []string
			f, err := os.Open(files[0])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var rec map[string]any
				if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
					t.Fatalf("log line is not JSON: %q", sc.Text())
				}
				msgs = append(msgs, rec["msg"].(string))
			}
			want := []string{"Infrastructure", "Infrastructure deployment completed", tt.wantFinal}
			if strings.Join(msgs, "|") != strings.Join(want, "|") {
				t.Errorf("log records = %q, want %q", msgs, want)
			}
		})
	}
}

func TestDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{"0.15.0": "v0.15.0", "v0.15.0-3-gabc": "v0.15.0-3-gabc", "dev": "dev", "": ""} {
		if got := displayVersion(in); got != want {
			t.Errorf("displayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRetryCommand(t *testing.T) {
	if got := retryCommand("nic deploy", ""); got != "nic deploy" {
		t.Errorf("without -f: %q", got)
	}
	if got := retryCommand("nic deploy", "prod.yaml"); got != "nic deploy -f prod.yaml" {
		t.Errorf("with -f: %q", got)
	}
}

func TestDestroyHeader(t *testing.T) {
	h := destroyHeader(output.Header{Target: "local"}, "acme", false)
	if !h.Danger || h.TargetPrep != "on" || h.Title != "Destroying acme" || h.Badge != "" {
		t.Errorf("destroy header = %+v", h)
	}
	h = destroyHeader(output.Header{Target: "local"}, "acme", true)
	if h.Danger || h.Badge != dryRunBadge || h.Command != "nic destroy --dry-run" {
		t.Errorf("dry-run destroy header = %+v", h)
	}
}

func TestClusterTarget(t *testing.T) {
	client, err := nic.NewClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		provider string
		block    map[string]any
		want     string
	}{
		{provider: "aws", block: map[string]any{"region": "us-west-2"}, want: "aws · us-west-2"},
		{provider: "azure", block: map[string]any{"region": "eastus"}, want: "azure · eastus"},
		{provider: "hetzner", block: map[string]any{"location": "ash"}, want: "hetzner · ash"},
		{provider: "existing", block: map[string]any{"context": "kind-x"}, want: "existing · context kind-x"},
		{provider: "local", block: map[string]any{}, want: "local"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			cfg := &config.NebariConfig{Cluster: &config.ClusterConfig{Providers: map[string]any{tt.provider: tt.block}}}
			if got := clusterTarget(context.Background(), client, cfg); got != tt.want {
				t.Errorf("clusterTarget() = %q, want %q", got, tt.want)
			}
		})
	}
	// Before validation the cluster block can be missing; the header then
	// names no target and validation reports the config next.
	if got := clusterTarget(context.Background(), client, &config.NebariConfig{}); got != "" {
		t.Errorf("clusterTarget() without a cluster = %q, want empty", got)
	}
}

func TestDeployHeader(t *testing.T) {
	h := deployHeader(output.Header{Target: "local"}, "acme", false)
	if h.Command != "nic deploy" || h.Title != "Deploying acme" || h.Badge != "" || h.Retry != "nic deploy" || h.Inspect == nil {
		t.Errorf("deploy header = %+v", h)
	}
	h = deployHeader(output.Header{Target: "local"}, "acme", true)
	if h.Command != "nic deploy --dry-run" || h.Title != "Planning the deploy of acme" || h.Badge != dryRunBadge || h.Retry != "nic deploy --dry-run" {
		t.Errorf("dry-run deploy header = %+v", h)
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestWriteGuidance: every format prints the same steps; pretty falls back to
// plain text when stdout isn't a terminal, as under go test.
func TestWriteGuidance(t *testing.T) {
	steps := []output.Step{{Title: "Sign in as admin", Lines: []output.StepLine{{Label: "password", Value: "nic outputs --show-secrets"}}}}
	want := "\nNext steps\n  1. Sign in as admin\n       password   nic outputs --show-secrets\n"
	for _, f := range []output.Format{output.FormatPretty, output.FormatPlain, output.FormatJSON, output.FormatGitHub} {
		t.Run(string(f), func(t *testing.T) {
			outputFormat = f
			t.Cleanup(func() { outputFormat = "" })
			if got := captureStdout(t, func() { writeGuidance(steps) }); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestStderrRouter: while a renderer is attached, log records are handed to
// it (which prints them above the live panel); otherwise they go to stderr.
func TestStderrRouter(t *testing.T) {
	var direct, rendered strings.Builder
	router := &stderrRouter{w: &direct}
	_, _ = router.Write([]byte("before\n"))

	r := output.NewPlain(&rendered, output.Options{})
	router.through(r)
	_, _ = router.Write([]byte("during\n"))
	router.through(nil)
	_, _ = router.Write([]byte("after\n"))

	if direct.String() != "before\nafter\n" || rendered.String() != "during\n" {
		t.Errorf("direct = %q, rendered = %q", direct.String(), rendered.String())
	}
}

func TestAPIWarnings(t *testing.T) {
	var buf strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(output.NewTextHandler(&buf, output.FormatPlain, false)))
	t.Cleanup(func() { slog.SetDefault(old) })

	w := &apiWarnings{}
	w.HandleWarningHeader(299, "", "v1 Endpoints is deprecated")
	w.HandleWarningHeader(299, "", "v1 Endpoints is deprecated")
	w.HandleWarningHeader(199, "", "not a warning header")
	if got := strings.Count(buf.String(), "Kubernetes API warning"); got != 1 {
		t.Errorf("logged %d warnings, want 1 (deduplicated, 299 only):\n%s", got, buf.String())
	}
}

// TestProgressCommands: only deploy and destroy render a status stream, so
// only they switch to the github format in GitHub Actions. nic outputs in
// particular must keep JSON there: deploy-nebari-action parses its stderr.
func TestProgressCommands(t *testing.T) {
	var progress []string
	for _, c := range NewRootCmd().Commands() {
		if c.Annotations[progressAnnotation] == "true" {
			progress = append(progress, c.Name())
		}
	}
	if strings.Join(progress, ",") != "deploy,destroy" {
		t.Errorf("commands marked as rendering progress = %v, want [deploy destroy]", progress)
	}
}
