package renderer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		t.Error("expected tree branch ├─ for non-last step")
	}
	if !strings.Contains(output, "└─") {
		t.Error("expected tree last └─ for final step")
	}
	if !strings.Contains(output, "completed") {
		t.Error("expected 'completed' status column")
	}
	if !strings.Contains(output, "failed") {
		t.Error("expected 'failed' status column")
	}
}

func TestPretty_LastStepUsesLastConnector(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.StartPhase("Test")
	r.EndStep(StepOK, 0, "first")
	r.EndStep(StepOK, 0, "second")
	r.EndStep(StepOK, 0, "third")
	r.EndPhase(PhaseOK, 0)

	output := buf.String()
	lines := strings.Split(output, "\n")

	// Find lines with step content
	var branchCount, lastCount int
	for _, line := range lines {
		if strings.Contains(line, "├─") {
			branchCount++
		}
		if strings.Contains(line, "└─") {
			lastCount++
		}
	}

	if branchCount != 2 {
		t.Errorf("expected 2 branch (├─) connectors, got %d", branchCount)
	}
	if lastCount != 1 {
		t.Errorf("expected 1 last (└─) connector, got %d", lastCount)
	}
}

func TestPretty_StepOutsidePhaseNoTreePrefix(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.EndStep(StepOK, time.Second, "Configuration validated")
	// Flush by calling Summary with empty items (triggers flushPending via another method)
	r.StartPhase("Infrastructure")
	r.EndPhase(PhaseOK, 0)

	output := buf.String()
	// The "Configuration validated" line should NOT have ├─ or └─
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Configuration validated") {
			if strings.Contains(line, "├─") || strings.Contains(line, "└─") {
				t.Errorf("step outside phase should not have tree prefix: %s", line)
			}
			break
		}
	}
}

func TestPretty_SeparatorAfterPhase(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.StartPhase("Infrastructure")
	r.EndStep(StepOK, time.Second, "Cluster created")
	r.EndPhase(PhaseOK, time.Second)

	r.EndStep(StepOK, 5*time.Minute, "Deployment complete")
	r.Summary(nil) // flush pending

	output := buf.String()
	lines := strings.Split(output, "\n")

	// Find the "Deployment complete" line and check for separator before it
	for i, line := range lines {
		if strings.Contains(line, "Deployment complete") {
			// The line before should be a separator (contains ───)
			if i > 0 && !strings.Contains(lines[i-1], "───") {
				t.Errorf("expected separator before out-of-phase step after phase, got: %q", lines[i-1])
			}
			break
		}
	}
}

func TestPretty_DetailContainment(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, true) // verbose mode

	r.StartPhase("Infrastructure")
	r.Detail("Installing k3s...")
	r.Detail("k3s installation completed")
	r.EndStep(StepOK, time.Second, "Cluster created")
	r.EndPhase(PhaseOK, time.Second)

	output := buf.String()

	// Should contain dotted separators (┄) around detail lines
	if !strings.Contains(output, "┄") {
		t.Error("detail block should be contained between dotted separators")
	}

	// Count dotted separator lines (should be 2: opening and closing)
	dotCount := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "┄┄┄") {
			dotCount++
		}
	}
	if dotCount != 2 {
		t.Errorf("expected 2 dotted separators (open+close), got %d", dotCount)
	}

	// Detail content should be present
	if !strings.Contains(output, "Installing k3s...") {
		t.Error("detail content should be present in verbose mode")
	}
}

func TestPretty_DetailNonVerbose(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, false)

	r.StartPhase("Test")
	r.Detail("Installing k3s...")
	r.EndStep(StepOK, 0, "done")
	r.EndPhase(PhaseOK, 0)

	output := buf.String()
	if strings.Contains(output, "Installing k3s...") {
		t.Error("non-verbose mode should suppress detail lines")
	}
	if strings.Contains(output, "┄") {
		t.Error("non-verbose mode should not show detail separators")
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
	if !strings.Contains(output, "Outputs:") {
		t.Error("summary should have Outputs header")
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
	if strings.Contains(output, "✓") || strings.Contains(output, "├") {
		t.Errorf("plain mode should not contain Unicode: %s", output)
	}
	// Should contain ASCII alternatives
	if !strings.Contains(output, "+") || !strings.Contains(output, "completed") {
		t.Errorf("plain mode should use ASCII symbols and status columns: %s", output)
	}
}

func TestPlain_NoColor(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, false)

	r.StartPhase("Test")
	r.EndStep(StepOK, 0, "done")
	r.EndPhase(PhaseOK, 0)

	output := buf.String()
	if strings.Contains(output, "\033[") {
		t.Error("plain mode should not contain ANSI escape codes")
	}
}

func TestPlain_DetailContainment(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, true) // verbose

	r.StartPhase("Test")
	r.Detail("some detail")
	r.EndStep(StepOK, 0, "done")
	r.EndPhase(PhaseOK, 0)

	output := buf.String()
	// ASCII dotSep is ".", should have containment dots
	dotCount := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "....") {
			dotCount++
		}
	}
	if dotCount != 2 {
		t.Errorf("expected 2 dot separator lines in plain mode, got %d", dotCount)
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
		{"\033[1m\033[36m▸\033[0m \033[1mInfra\033[0m", 7}, // "▸ Infra"
		{"", 0},
	}
	for _, tt := range tests {
		got := visibleLen(tt.input)
		if got != tt.want {
			t.Errorf("visibleLen(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// --- Full deployment simulation ---

func TestPretty_FullDeploymentFlow(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, true) // verbose

	// Config phase (outside any phase)
	r.EndStep(StepOK, 100*time.Millisecond, "Configuration validated")
	r.Info("Deploying nic-nebari (hetzner)")

	// Infrastructure phase
	r.StartPhase("Infrastructure")
	r.EndStep(StepOK, 800*time.Millisecond, "SSH keys verified")
	r.Detail("[master1] Installing k3s...")
	r.Detail("[master1] k3s installation completed")
	r.EndStep(StepOK, 3*time.Minute+38*time.Second, "Cluster created")
	r.EndStep(StepOK, 400*time.Millisecond, "Kubeconfig saved")
	r.EndPhase(PhaseOK, 4*time.Minute)

	// ArgoCD phase
	r.StartPhase("ArgoCD")
	r.EndStep(StepOK, 12100*time.Millisecond, "Helm chart installed")
	r.EndStep(StepOK, 15900*time.Millisecond, "Root app-of-apps applied")
	r.EndPhase(PhaseOK, 28*time.Second)

	// Final
	r.EndStep(StepOK, 5*time.Minute+29*time.Second, "Deployment complete")

	r.Summary([]SummaryItem{
		{Label: "ArgoCD", Value: "https://argocd.nebari.example.com"},
		{Label: "Keycloak", Value: "https://keycloak.example.com"},
		{Label: "Kubeconfig", Value: "~/.cache/nic/hetzner-k3s/nic-nebari/kubeconfig"},
	})

	output := buf.String()

	// Verify structure
	checks := []struct {
		name    string
		pattern string
	}{
		{"config step", "Configuration validated"},
		{"infra phase", "Infrastructure"},
		{"branch connector", "├─"},
		{"last connector", "└─"},
		{"detail open", "┄┄┄"},
		{"detail content", "Installing k3s"},
		{"detail content", "k3s installation completed"},
		{"argocd phase", "ArgoCD"},
		{"separator before final", "───"},
		{"final step", "Deployment complete"},
		{"outputs header", "Outputs:"},
		{"argocd url", "https://argocd.nebari.example.com"},
		{"keycloak url", "https://keycloak.example.com"},
		{"kubeconfig path", "kubeconfig"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.pattern) {
			t.Errorf("[%s] expected output to contain %q", c.name, c.pattern)
		}
	}
}

// --- TUI model tests (unit-test the model without a real terminal) ---

// updateTui is a test helper that calls Update and returns the model.
func updateTui(m tuiModel, msg tea.Msg) tuiModel {
	result, _ := m.Update(msg)
	return result.(tuiModel)
}

func TestTuiModel_PhaseTracking(t *testing.T) {
	m := newTuiModel()
	m.width = 120
	m.height = 40

	m = updateTui(m, tuiPhaseStartMsg{name: "Infrastructure"})
	if len(m.phases) != 1 || m.phases[0].Status != phaseRunning {
		t.Fatal("expected one running phase")
	}

	m = updateTui(m, tuiPhaseEndMsg{status: PhaseOK})
	if m.phases[0].Status != phaseDone {
		t.Error("expected phase to be done")
	}

	m = updateTui(m, tuiPhaseStartMsg{name: "ArgoCD"})
	if len(m.phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(m.phases))
	}
	if m.phases[0].Status != phaseDone {
		t.Error("first phase should remain done")
	}
	if m.phases[1].Status != phaseRunning {
		t.Error("second phase should be running")
	}
}

func TestTuiModel_EventRingBuffer(t *testing.T) {
	m := newTuiModel()
	m.width = 120
	m.height = 40

	m = updateTui(m, tuiInfoMsg{message: "hello"})
	m = updateTui(m, tuiWarnMsg{message: "careful"})
	m = updateTui(m, tuiStepEndMsg{status: StepOK, detail: "done"})

	if m.events.size() != 3 {
		t.Fatalf("expected 3 events, got %d", m.events.size())
	}
	if m.events.get(0).Level != "info" {
		t.Error("expected info event")
	}
	if m.events.get(1).Level != "warn" {
		t.Error("expected warn event")
	}
	if m.events.get(2).Level != "ok" {
		t.Error("expected ok event")
	}
	if m.warnCount != 1 {
		t.Errorf("expected warnCount=1, got %d", m.warnCount)
	}
	if m.stepCount != 1 {
		t.Errorf("expected stepCount=1, got %d", m.stepCount)
	}
}

func TestTuiModel_SummaryMarksDone(t *testing.T) {
	m := newTuiModel()
	m.width = 120
	m.height = 40

	m = updateTui(m, tuiPhaseStartMsg{name: "Test"})
	m = updateTui(m, tuiSummaryMsg{items: []SummaryItem{{Label: "URL", Value: "https://example.com"}}})

	if !m.done {
		t.Error("expected done=true after summary")
	}
	if m.phases[0].Status != phaseDone {
		t.Error("running phase should be marked done after summary")
	}
	if len(m.summaryItems) != 1 {
		t.Error("expected summary items to be stored")
	}
}

func TestTuiModel_CtrlCAlwaysQuits(t *testing.T) {
	m := newTuiModel()
	m.width = 120
	m.height = 40
	m.done = false // deployment in progress

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("ctrl+c should return tea.Quit even when not done")
	}
}

func TestTuiModel_ViewRendersWithoutPanic(t *testing.T) {
	m := newTuiModel()
	m.width = 120
	m.height = 40

	m = updateTui(m, tuiPhaseStartMsg{name: "Infrastructure"})
	m = updateTui(m, tuiStepEndMsg{status: StepOK, elapsed: time.Second, detail: "Cluster created"})
	m = updateTui(m, tuiDetailMsg{line: "some detail output"})
	m = updateTui(m, tuiPhaseEndMsg{status: PhaseOK})

	view := m.View()
	if len(view) == 0 {
		t.Error("view should not be empty")
	}
	if !strings.Contains(view, "Infrastructure") {
		t.Error("view should contain phase name")
	}
	if !strings.Contains(view, "NIC Deploy") {
		t.Error("view should contain NIC Deploy title")
	}
	if !strings.Contains(view, "Activity") {
		t.Error("view should contain Activity title")
	}
	if !strings.Contains(view, "Elapsed") {
		t.Error("view should contain Elapsed timer")
	}
	// Steps should only appear in right panel (activity), not duplicated in left
	if !strings.Contains(view, "Cluster created") {
		t.Error("right panel should show step detail")
	}
}

func TestTuiModel_ProjectExtraction(t *testing.T) {
	m := newTuiModel()
	m.width = 120
	m.height = 40

	m = updateTui(m, tuiInfoMsg{message: "Deploying my-project (hetzner)"})

	if m.project != "my-project" {
		t.Errorf("expected project='my-project', got %q", m.project)
	}
	if m.provider != "hetzner" {
		t.Errorf("expected provider='hetzner', got %q", m.provider)
	}

	view := m.View()
	if !strings.Contains(view, "my-project") {
		t.Error("view should show extracted project name")
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
