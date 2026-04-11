package renderer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// --- LineWriter tests ---

func TestLineWriter_SplitsLines(t *testing.T) {
	var lines []string
	mock := &mockRenderer{detailFn: func(line string) { lines = append(lines, line) }}
	w := &lineWriter{r: mock}

	_, _ = w.Write([]byte("first\nsecond\n"))

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "first" || lines[1] != "second" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestLineWriter_BuffersPartialLine(t *testing.T) {
	var lines []string
	mock := &mockRenderer{detailFn: func(line string) { lines = append(lines, line) }}
	w := &lineWriter{r: mock}

	_, _ = w.Write([]byte("foo"))
	_, _ = w.Write([]byte("bar\n"))

	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if lines[0] != "foobar" {
		t.Errorf("expected 'foobar', got %q", lines[0])
	}
}

func TestLineWriter_FlushPartial(t *testing.T) {
	var lines []string
	mock := &mockRenderer{detailFn: func(line string) { lines = append(lines, line) }}
	w := &lineWriter{r: mock}

	_, _ = w.Write([]byte("trailing"))
	w.Flush()

	if len(lines) != 1 || lines[0] != "trailing" {
		t.Errorf("expected ['trailing'], got %v", lines)
	}
}

func TestLineWriter_SkipsEmptyLines(t *testing.T) {
	var lines []string
	mock := &mockRenderer{detailFn: func(line string) { lines = append(lines, line) }}
	w := &lineWriter{r: mock}

	_, _ = w.Write([]byte("\n\nfoo\n\n"))

	if len(lines) != 1 || lines[0] != "foo" {
		t.Errorf("expected ['foo'], got %v", lines)
	}
}

// --- JSON renderer tests ---

func TestJSON_StartPhase(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSON(&buf)
	r.StartPhase("Infrastructure")

	assertJSONContains(t, buf.String(), "phase", "Infrastructure")
}

func TestJSON_EndStep(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSON(&buf)
	r.EndStep(StepOK, 2*time.Second, "Cluster created")

	output := buf.String()
	assertJSONContains(t, output, "status", "ok")
	assertJSONContains(t, output, "detail", "Cluster created")
}

func TestJSON_Error(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSON(&buf)
	r.Error(errors.New("something failed"), "try again")

	output := buf.String()
	assertJSONContains(t, output, "error", "something failed")
	assertJSONContains(t, output, "hint", "try again")
}

func TestJSON_Version(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSON(&buf)
	r.Version("1.0.0", "abc123", "1.9.0", []string{"aws", "hetzner"}, []string{"cloudflare"})

	output := buf.String()
	assertJSONContains(t, output, "version", "1.0.0")
	assertJSONContains(t, output, "commit", "abc123")
}

// --- Pretty renderer tests ---

func TestPretty_PhaseTree(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.StartPhase("Infrastructure")
	r.EndStep(StepOK, time.Second, "SSH keys verified")
	r.EndStep(StepFailed, 500*time.Millisecond, "Cluster creation failed")
	r.EndPhase(PhaseFailed, 2*time.Second)

	output := buf.String()
	if !strings.Contains(output, "●") {
		t.Error("expected phase bullet ●")
	}
	if !strings.Contains(output, "Infrastructure") {
		t.Error("expected phase name")
	}
	if !strings.Contains(output, "✓") {
		t.Error("expected success checkmark ✓")
	}
	if !strings.Contains(output, "✗") {
		t.Error("expected failure cross ✗")
	}
	if !strings.Contains(output, "├─") {
		t.Error("expected tree branch ├─")
	}
}

func TestPretty_DetailVerbose(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, true)

	r.Detail("Installing k3s...")
	output := buf.String()
	if !strings.Contains(output, "Installing k3s...") {
		t.Error("verbose mode should show detail lines")
	}
}

func TestPretty_DetailNonVerbose(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.Detail("Installing k3s...")
	if buf.Len() != 0 {
		t.Error("non-verbose mode should suppress detail lines")
	}
}

func TestPretty_Summary(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.Summary([]SummaryItem{
		{Label: "ArgoCD", Value: "https://argocd.example.com"},
		{Label: "Keycloak", Value: "https://keycloak.example.com"},
	})

	output := buf.String()
	if !strings.Contains(output, "ArgoCD") || !strings.Contains(output, "https://argocd.example.com") {
		t.Error("summary should contain ArgoCD entry")
	}
	if !strings.Contains(output, "┌") || !strings.Contains(output, "└") {
		t.Error("summary should have box borders")
	}
}

func TestPretty_Error(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.Error(errors.New("deployment failed"), "Check your credentials")
	output := buf.String()
	if !strings.Contains(output, "deployment failed") {
		t.Error("error message should be shown")
	}
	if !strings.Contains(output, "Check your credentials") {
		t.Error("hint should be shown")
	}
}

// --- Plain renderer tests ---

func TestPlain_ASCIIOnly(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, false)

	r.StartPhase("Infrastructure")
	r.EndStep(StepOK, time.Second, "Cluster created")
	r.EndPhase(PhaseOK, time.Second)

	output := buf.String()
	// Should not contain Unicode tree characters
	if strings.Contains(output, "✓") || strings.Contains(output, "●") || strings.Contains(output, "├") {
		t.Errorf("plain mode should not contain Unicode: %s", output)
	}
	// Should contain ASCII alternatives
	if !strings.Contains(output, "[OK]") || !strings.Contains(output, "*") {
		t.Errorf("plain mode should use ASCII symbols: %s", output)
	}
}

func TestPlain_NoColor(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, false)

	r.StartPhase("Test")
	r.EndStep(StepOK, 0, "done")

	output := buf.String()
	if strings.Contains(output, "\033[") {
		t.Error("plain mode should not contain ANSI escape codes")
	}
}

// --- visibleLen tests ---

func TestVisibleLen(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"\033[31mhello\033[0m", 5},
		{"\033[1m\033[36m●\033[0m \033[1mInfra\033[0m", 7}, // "● Infra"
		{"", 0},
	}
	for _, tt := range tests {
		got := visibleLen(tt.input)
		if got != tt.want {
			t.Errorf("visibleLen(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// --- Helpers ---

type mockRenderer struct {
	detailFn func(string)
}

func (m *mockRenderer) StartPhase(string)                                       {}
func (m *mockRenderer) EndPhase(PhaseStatus, time.Duration)                     {}
func (m *mockRenderer) StartStep(string)                                        {}
func (m *mockRenderer) EndStep(StepStatus, time.Duration, string)               {}
func (m *mockRenderer) Detail(line string)                                      { m.detailFn(line) }
func (m *mockRenderer) Summary([]SummaryItem)                                   {}
func (m *mockRenderer) Error(error, string)                                     {}
func (m *mockRenderer) Warn(string)                                             {}
func (m *mockRenderer) Info(string)                                             {}
func (m *mockRenderer) Version(string, string, string, []string, []string)      {}
func (m *mockRenderer) Confirm(string, map[string]string, string) (bool, error) { return false, nil }
func (m *mockRenderer) DetailWriter() io.Writer                                 { return &lineWriter{r: m} }

func assertJSONContains(t *testing.T, output, key, value string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	lastLine := lines[len(lines)-1]
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lastLine), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v\nline: %s", err, lastLine)
	}
	got, ok := parsed[key]
	if !ok {
		t.Errorf("JSON missing key %q in: %s", key, lastLine)
		return
	}
	if s, ok := got.(string); !ok || s != value {
		t.Errorf("JSON key %q = %v, want %q", key, got, value)
	}
}
