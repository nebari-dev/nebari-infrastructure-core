package output

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var t0 = time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

func phaseAt(name string, d time.Duration) status.Update {
	u := status.NewUpdate(status.LevelProgress, name).
		WithResource(status.ResourcePhase).
		WithMetadata(status.MetadataKeyPhase, name)
	u.Timestamp = at(d)
	return u
}

func updateAt(level status.Level, msg string, d time.Duration) status.Update {
	u := status.NewUpdate(level, msg)
	u.Timestamp = at(d)
	return u
}

var testHeader = Header{
	Command:    "nic deploy",
	Title:      "Deploying acme",
	TargetPrep: "to",
	Target:     "aws \u00b7 us-west-2",
	ConfigFile: "config.yaml",
	Version:    "v0.15.0",
	LogPath:    "/logs/acme.jsonl",
	Done:       "Deployed acme",
	Failed:     "Deploy failed",
	Retry:      "nic deploy",
	Inspect:    map[string]string{"Argo CD": "kubectl get pods -n argocd"},
}

// testLink is what run passes to Finish for the success result line.
const testLink = "https://acme.example.com"

// deployScript is a short deploy: a quiet phase, a successful one, and one
// with a warning, plus an informational update and a line of tool output.
func deployScript() []status.Update {
	return []status.Update{
		phaseAt("Configuration", 0),
		updateAt(status.LevelInfo, "Starting deployment", 0),
		phaseAt("Infrastructure", 100*time.Millisecond),
		updateAt(status.LevelInfo, "Plan: 1 to add, 0 to change, 1 to destroy.", time.Second).
			WithMetadata(status.MetadataKeyDetail, json.RawMessage(`{}`)),
		updateAt(status.LevelSuccess, "Infrastructure deployment completed", 9*time.Second),
		phaseAt("Argo CD", 10*time.Second),
		updateAt(status.LevelWarning, "Failed to install Argo CD", 12*time.Second).WithMetadata("error", "boom"),
	}
}

func run(r *Renderer, updates []status.Update, err error) {
	r.Begin(testHeader)
	for _, u := range updates {
		r.Handle(u)
	}
	r.Finish(err, testLink)
}

// fixedClock returns a clock that reads t0 on its first call (Begin) and
// t0+15s afterwards (Finish).
func fixedClock() func() time.Time {
	calls := 0
	return func() time.Time {
		calls++
		if calls == 1 {
			return t0
		}
		return at(15 * time.Second)
	}
}

func TestPlainRenderer(t *testing.T) {
	tests := []struct {
		name    string
		new     func(*bytes.Buffer, Options) *Renderer
		verbose bool
		err     error
		want    string
	}{
		{
			name: "plain",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			want: `13:00:00 nic deploy: Deploying acme to aws · us-west-2
13:00:00 config: config.yaml (nic v0.15.0)
13:00:00 log: /logs/acme.jsonl
13:00:00 [..]    Configuration
13:00:00 [ok]    Configuration (<1s)
13:00:00 [..]    Infrastructure
13:00:09 [ok]    Infrastructure: Infrastructure deployment completed
13:00:10 [ok]    Infrastructure (9s)
13:00:10 [..]    Argo CD
13:00:12 [warn]  Argo CD: Failed to install Argo CD: boom
13:00:15 [warn]  Argo CD (5s)
13:00:15 [warn]  Deployed acme with warnings: https://acme.example.com (15s)
`,
		},
		{
			name:    "plain verbose shows info and tool output",
			new:     func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			verbose: true,
			want: `13:00:00 nic deploy: Deploying acme to aws · us-west-2
13:00:00 config: config.yaml (nic v0.15.0)
13:00:00 log: /logs/acme.jsonl
13:00:00 [..]    Configuration
13:00:00 [info]  Configuration: Starting deployment
13:00:00 [ok]    Configuration (<1s)
13:00:00 [..]    Infrastructure
13:00:01 [info]  Infrastructure: Plan: 1 to add, 0 to change, 1 to destroy.
13:00:09 [ok]    Infrastructure: Infrastructure deployment completed
13:00:10 [ok]    Infrastructure (9s)
13:00:10 [..]    Argo CD
13:00:12 [warn]  Argo CD: Failed to install Argo CD: boom
13:00:15 [warn]  Argo CD (5s)
13:00:15 [warn]  Deployed acme with warnings: https://acme.example.com (15s)
`,
		},
		{
			name: "failure marks the running phase and the result",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			err:  errors.New("install argo cd: timed out"),
			want: `13:00:00 nic deploy: Deploying acme to aws · us-west-2
13:00:00 config: config.yaml (nic v0.15.0)
13:00:00 log: /logs/acme.jsonl
13:00:00 [..]    Configuration
13:00:00 [ok]    Configuration (<1s)
13:00:00 [..]    Infrastructure
13:00:09 [ok]    Infrastructure: Infrastructure deployment completed
13:00:10 [ok]    Infrastructure (9s)
13:00:10 [..]    Argo CD
13:00:12 [warn]  Argo CD: Failed to install Argo CD: boom
13:00:15 [fail]  Argo CD (5s)
13:00:15 [fail]  Deploy failed: Argo CD (15s)
13:00:15 inspect: kubectl get pods -n argocd
13:00:15 log: /logs/acme.jsonl
13:00:15 retry: nic deploy
`,
		},
		{
			name: "interrupt",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			err:  context.Canceled,
			want: `13:00:00 nic deploy: Deploying acme to aws · us-west-2
13:00:00 config: config.yaml (nic v0.15.0)
13:00:00 log: /logs/acme.jsonl
13:00:00 [..]    Configuration
13:00:00 [ok]    Configuration (<1s)
13:00:00 [..]    Infrastructure
13:00:09 [ok]    Infrastructure: Infrastructure deployment completed
13:00:10 [ok]    Infrastructure (9s)
13:00:10 [..]    Argo CD
13:00:12 [warn]  Argo CD: Failed to install Argo CD: boom
13:00:15 [warn]  Argo CD (5s)
13:00:15 [warn]  Interrupted: Argo CD (15s)
13:00:15 log: /logs/acme.jsonl
13:00:15 retry: nic deploy
`,
		},
		{
			name: "github groups phases and annotates warnings",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewGitHub(b, o) },
			want: `13:00:00 nic deploy: Deploying acme to aws · us-west-2
13:00:00 config: config.yaml (nic v0.15.0)
13:00:00 log: /logs/acme.jsonl
::group::Configuration
13:00:00 [info]  Configuration: Starting deployment
::endgroup::
13:00:00 [ok]    Configuration (<1s)
::group::Infrastructure
13:00:01 [info]  Infrastructure: Plan: 1 to add, 0 to change, 1 to destroy.
13:00:09 [ok]    Infrastructure: Infrastructure deployment completed
::endgroup::
13:00:10 [ok]    Infrastructure (9s)
::group::Argo CD
::warning title=Argo CD::Failed to install Argo CD: boom
::endgroup::
13:00:15 [warn]  Argo CD (5s)
13:00:15 [warn]  Deployed acme with warnings: https://acme.example.com (15s)
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := tt.new(&buf, Options{Verbose: tt.verbose, Now: fixedClock()})
			run(r, deployScript(), tt.err)
			if buf.String() != tt.want {
				t.Errorf("output differs\n--- got\n%s\n--- want\n%s", buf.String(), tt.want)
			}
		})
	}
}

func TestPrettyRenderer(t *testing.T) {
	const termWidth = 60
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: termWidth, Now: fixedClock()})
	run(r, deployScript(), nil)
	out := buf.String()

	for _, want := range []string{
		glyphPrompt + " nic deploy",
		"  Deploying acme to aws \u00b7 us-west-2",
		"  config  config.yaml \u00b7 nic v0.15.0",
		"  log     /logs/acme.jsonl",
		"  " + glyphOK + " Configuration",
		boxTopLeft + boxHorizontal + " " + glyphOK + " Infrastructure ",
		boxVertical + " " + glyphOK + " Infrastructure deployment completed",
		boxTopLeft + boxHorizontal + " " + glyphWarn + " Argo CD ",
		glyphWarn + " Failed to install Argo CD: boom",
		// The script has a warning, and at 60 columns the link goes below.
		"  " + glyphWarn + " Deployed acme with warnings ",
		"\n    " + testLink + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Starting deployment") || strings.Contains(out, "Plan: 1 to add") {
		t.Errorf("informational updates and tool output should be hidden without -v:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("no colour or cursor control expected with Color and Live off:\n%q", out)
	}

	// Panels and right-aligned rows fill the terminal width exactly.
	for _, line := range strings.Split(out, "\n") {
		framed := strings.Contains(line, boxVertical) || strings.Contains(line, boxTopLeft) || strings.Contains(line, boxBottomLeft)
		if framed && width(line) != termWidth {
			t.Errorf("panel line is %d columns, want %d: %q", width(line), termWidth, line)
		}
		if width(line) > termWidth {
			t.Errorf("line wider than the terminal: %q", line)
		}
	}
}

// eraseLive is how the live panel erases its n lines before a redraw.
func eraseLive(n int) string { return fmt.Sprintf("\x1b[%dA\r\x1b[J", n) }

// TestPrettyRendererLivePanel: on a terminal the running phase's panel is on
// screen from the start, fills with its steps, and is redrawn in place.
func TestPrettyRendererLivePanel(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	buf.Reset()

	r.Handle(phaseAt("Infrastructure", 0))
	opened := buf.String()
	if !strings.Contains(opened, boxTopLeft+boxHorizontal+" "+spinner[0]+" Infrastructure ") {
		t.Fatalf("the panel should open with the phase, running:\n%s", opened)
	}
	if got := strings.Count(opened, "\n"); got != 2 {
		t.Fatalf("an empty live panel is a title and a bottom border, got %d lines:\n%s", got, opened)
	}

	buf.Reset()
	r.Handle(updateAt(status.LevelSuccess, "Kind cluster created", 3*time.Second))
	r.Handle(updateAt(status.LevelProgress, "Creating node group", 4*time.Second))
	redraw := buf.String()
	for _, want := range []string{eraseLive(2), glyphOK + " Kind cluster created", spinner[0] + " Creating node group", " 4s "} {
		if !strings.Contains(redraw, want) {
			t.Errorf("redrawn panel lacks %q:\n%q", want, redraw)
		}
	}

	buf.Reset()
	r.Handle(phaseAt("GitOps", 10*time.Second))
	closed := buf.String()
	if !strings.HasPrefix(closed, eraseLive(4)) {
		t.Errorf("closing should erase the 4-line live panel first, got %q", closed)
	}
	for _, want := range []string{glyphOK + " Infrastructure ", " 10s ", spinner[0] + " GitOps "} {
		if !strings.Contains(closed, want) {
			t.Errorf("after the phase closes, output lacks %q:\n%s", want, closed)
		}
	}
	if strings.Contains(stripANSI(closed), "Creating node group") {
		t.Errorf("the activity line belongs to the live panel only:\n%s", closed)
	}
}

// TestPrettyRendererLivePanelHeight: a live panel keeps to the newest lines,
// so it always fits on screen; the closed panel lists every step.
func TestPrettyRendererLivePanelHeight(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Height: 15, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Foundational services", 0))
	for i := range 10 {
		r.Handle(updateAt(status.LevelSuccess, fmt.Sprintf("step %d", i), time.Second))
	}
	buf.Reset()
	r.Handle(updateAt(status.LevelSuccess, "step 10", 2*time.Second))
	live := stripANSI(buf.String())
	if !strings.Contains(live, "\u2026 9 earlier") || !strings.Contains(live, "step 10") || strings.Contains(live, "step 0\n") {
		t.Errorf("live panel should show a count and the newest steps:\n%s", live)
	}

	buf.Reset()
	r.Finish(nil, "")
	if final := stripANSI(buf.String()); strings.Count(final, "step ") != 11 {
		t.Errorf("the closed panel should list all 11 steps:\n%s", final)
	}
}

func TestRendererFinishIsFinal(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, Options{Now: fixedClock()})
	run(r, nil, nil)
	before := buf.String()

	r.Handle(phaseAt("Late", time.Second))
	r.Finish(errors.New("late"), "")
	if buf.String() != before {
		t.Errorf("output after Finish:\n%s", strings.TrimPrefix(buf.String(), before))
	}
}

// TestRendererConcurrentUse runs Handle on one goroutine, as the status
// handler does, while Pause and Resume run on another, as the destroy prompt
// does. Both start together and Finish waits for them, so the calls really
// interleave. Run with -race: removing any of the locks makes it fail.
func TestRendererConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true})
	r.Begin(testHeader)
	r.Handle(phaseAt("phase", 0))

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 200 {
			r.Handle(status.NewUpdate(status.LevelProgress, "working"))
			r.Handle(status.NewUpdate(status.LevelSuccess, "step"))
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 200 {
			r.Pause()
			r.Resume()
		}
	}()
	close(start)
	wg.Wait()
	r.Finish(nil, "")
}

// TestRendererThroughStatusHandler drives a Renderer the way the CLI does:
// producers send on the status channel from their own goroutines and the
// channel's single consumer calls Handle.
func TestRendererThroughStatusHandler(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, Options{})
	r.Begin(testHeader)
	ctx, cleanup := status.StartHandler(context.Background(), r.Handle)

	status.Phase(ctx, "Infrastructure")
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				status.Success(ctx, fmt.Sprintf("producer %d", i))
			}
		}()
	}
	wg.Wait()
	cleanup()
	r.Finish(nil, "")

	if got := strings.Count(buf.String(), "[ok]    Infrastructure: producer"); got != 100 {
		t.Errorf("rendered %d steps, want 100:\n%s", got, buf.String())
	}
}

func TestRendererPauseKeepsTheLivePanelOff(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Configuration", 0))
	r.Handle(updateAt(status.LevelInfo, "Starting destruction", time.Second))

	r.Pause()
	if !strings.HasSuffix(buf.String(), eraseLive(3)) {
		t.Fatalf("Pause should erase the live panel, got %q", buf.String())
	}
	buf.Reset()
	buf.WriteString("Type 'yes' to confirm: ")
	// An update still queued on the channel when the prompt went up.
	r.Handle(updateAt(status.LevelInfo, "Provider selected", 2*time.Second))
	if got := buf.String(); got != "Type 'yes' to confirm: " {
		t.Fatalf("a queued update redrew over the prompt: %q", got)
	}

	r.Resume()
	r.Handle(updateAt(status.LevelInfo, "Suspending Argo CD", 3*time.Second))
	if !strings.Contains(buf.String(), "Suspending Argo CD") {
		t.Errorf("the live panel should return after Resume, got %q", buf.String())
	}
}

func TestRendererDryRunShowsInfo(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, Options{DryRun: true, Now: fixedClock()})
	run(r, deployScript(), nil)
	out := buf.String()
	if !strings.Contains(out, "[info]  Configuration: Starting deployment") {
		t.Errorf("a dry run should list informational updates:\n%s", out)
	}
	if strings.Contains(out, "Plan: 1 to add") {
		t.Errorf("tool output should stay behind --verbose in a dry run:\n%s", out)
	}
}

func TestRendererSanitizesText(t *testing.T) {
	tests := []struct {
		name string
		new  func(*bytes.Buffer) *Renderer
		msg  string
		want string
		deny []string
	}{
		{
			name: "control characters are dropped and tabs become spaces",
			new:  func(b *bytes.Buffer) *Renderer { return NewPlain(b, Options{}) },
			msg:  "done\tin \x1b[31mred\x1b[0m",
			want: "[ok]    done in [31mred[0m",
			deny: []string{"\x1b", "\t"},
		},
		{
			name: "github keeps a multi-line step on one line",
			new:  func(b *bytes.Buffer) *Renderer { return NewGitHub(b, Options{}) },
			msg:  "first line\n::add-mask::secret",
			want: "[ok]    first line ::add-mask::secret",
			deny: []string{"\n::add-mask"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := tt.new(&buf)
			r.Handle(status.NewUpdate(status.LevelSuccess, tt.msg))
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("got %q, want it to contain %q", buf.String(), tt.want)
			}
			for _, d := range tt.deny {
				if strings.Contains(buf.String(), d) {
					t.Errorf("output %q still contains %q", buf.String(), d)
				}
			}
		})
	}
}

// TestPrettyRendererWithoutLivePanel: off a terminal there is no live panel,
// so the running phase is named when it starts, and warnings show as they
// happen instead of only in the panel at the end of the phase.
func TestPrettyRendererWithoutLivePanel(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Argo CD", 0))
	r.Handle(updateAt(status.LevelWarning, "Failed to install Argo CD", time.Second))

	out := buf.String()
	for _, want := range []string{spinner[0] + " Argo CD", glyphWarn + " Failed to install Argo CD"} {
		if !strings.Contains(out, want) {
			t.Errorf("before the phase ends, output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, boxTopLeft) {
		t.Errorf("the panel should wait for the phase to end:\n%s", out)
	}
}

// TestPrettyRendererTicks: during a silent wait the ticker keeps the live
// panel's spinner and elapsed time moving, and it stops at Finish. Run with -race.
func TestPrettyRendererTicks(t *testing.T) {
	var mu sync.Mutex
	elapsed := time.Duration(0)
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		elapsed += time.Second
		return t0.Add(elapsed)
	}
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Tick: 5 * time.Millisecond, Now: now})
	r.Begin(testHeader)
	r.Handle(phaseAt("Infrastructure", 0))
	time.Sleep(60 * time.Millisecond)
	r.Finish(nil, "")

	out := stripANSI(buf.String())
	frames := map[string]bool{}
	for _, f := range spinner {
		if strings.Contains(out, f+" Infrastructure ") {
			frames[f] = true
		}
	}
	if len(frames) < 3 {
		t.Errorf("the ticker should have advanced the spinner several times:\n%s", out)
	}
	after := buf.Len()
	time.Sleep(30 * time.Millisecond)
	if buf.Len() != after {
		t.Errorf("the ticker kept drawing after Finish")
	}
}

// TestPrettyRendererActivityClearsOnResult: the activity line describes work
// in flight, so a result for it replaces it.
func TestPrettyRendererActivityClearsOnResult(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Argo CD", 0))
	r.Handle(updateAt(status.LevelProgress, "Waiting for cluster to be ready", time.Second))
	buf.Reset()
	r.Handle(updateAt(status.LevelSuccess, "Cluster is ready", 2*time.Second))
	if out := stripANSI(buf.String()); strings.Contains(out, "Waiting for cluster") {
		t.Errorf("the activity line should clear once its result lands:\n%s", out)
	}
}

// TestPrettyRendererFailure: a failed run names the failing phase on the
// result line and says where to look next.
func TestPrettyRendererFailure(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Now: fixedClock()})
	run(r, deployScript(), errors.New("install argo cd: timed out"))
	out := buf.String()
	for _, want := range []string{
		boxTopLeft + boxHorizontal + " " + glyphFail + " Argo CD ",
		"  " + glyphFail + " Deploy failed \u00b7 Argo CD",
		"    inspect  kubectl get pods -n argocd",
		"    log      /logs/acme.jsonl",
		"    retry    nic deploy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, testLink) {
		t.Errorf("the link belongs to a successful run only:\n%s", out)
	}
}

// TestPrettyRendererHeaderBadge: a badge sits at the right edge of the title
// line.
func TestPrettyRendererHeaderBadge(t *testing.T) {
	const termWidth = 70
	h := testHeader
	h.Badge = "DRY RUN"
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: termWidth, Now: fixedClock()})
	r.Begin(h)
	title := strings.Split(buf.String(), "\n")[1]
	if !strings.HasPrefix(title, "  Deploying acme to aws") || !strings.HasSuffix(title, " DRY RUN") || width(title) != termWidth {
		t.Errorf("title line = %q (%d columns), want the badge right-aligned at %d", title, width(title), termWidth)
	}
}

// TestPrettyRendererHeaderLongTarget: a target too long for the title line,
// such as a kubeconfig context ARN, is cut so the badge stays on the line.
func TestPrettyRendererHeaderLongTarget(t *testing.T) {
	const termWidth = 70
	h := testHeader
	h.Target = "existing \u00b7 context arn:aws:eks:us-west-2:123456789012:cluster/my-nebari"
	h.Badge = "DRY RUN"
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: termWidth, Now: fixedClock()})
	r.Begin(h)
	title := strings.Split(buf.String(), "\n")[1]
	if width(title) != termWidth || !strings.HasSuffix(title, " DRY RUN") || !strings.Contains(title, "context arn:aws:eks:") || !strings.Contains(title, "\u2026") {
		t.Errorf("title line = %q (%d columns), want it cut to %d with the badge kept", title, width(title), termWidth)
	}
}

// TestPrettyRendererHeaderDanger: a destructive command's title is drawn in
// the failure colour, anything else in the active one.
func TestPrettyRendererHeaderDanger(t *testing.T) {
	for _, danger := range []bool{false, true} {
		h := testHeader
		h.Danger = danger
		var buf bytes.Buffer
		r := NewPretty(&buf, Options{Width: 70, Color: true, TrueColor: true, Now: fixedClock()})
		r.Begin(h)
		want := truePalette.act
		if danger {
			want = truePalette.fail
		}
		if title := strings.Split(buf.String(), "\n")[1]; !strings.Contains(title, truePalette.bold+want+"Deploying acme") {
			t.Errorf("danger=%v: title %q isn't drawn in %q", danger, title, want)
		}
	}
}

// TestRendererResultOutcome: the result line follows the worst phase, so a
// run that returns no error but had warnings doesn't end on a clean check.
func TestRendererResultOutcome(t *testing.T) {
	tests := []struct {
		name  string
		level status.Level
		want  string
	}{
		{name: "clean run", level: status.LevelSuccess, want: "[ok]    Deployed acme: https://acme.example.com (15s)\n"},
		{name: "warning", level: status.LevelWarning, want: "[warn]  Deployed acme with warnings: https://acme.example.com (15s)\n"},
		{name: "error step without a returned error", level: status.LevelError, want: "[warn]  Deployed acme with warnings: https://acme.example.com (15s)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			run(NewPlain(&buf, Options{Now: fixedClock()}), []status.Update{
				phaseAt("DNS", 0),
				updateAt(tt.level, "DNS records", time.Second),
			}, nil)
			if !strings.HasSuffix(buf.String(), tt.want) {
				t.Errorf("output ends\n%s\nwant it to end with\n%s", buf.String(), tt.want)
			}
		})
	}
}

// TestPrettyRendererInterrupt: an interrupt stops the running phase rather
// than failing it, and offers the log and a retry but nothing to inspect.
func TestPrettyRendererInterrupt(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 80, Now: fixedClock()})
	run(r, []status.Update{phaseAt("Argo CD", 0), updateAt(status.LevelSuccess, "Cluster is ready", time.Second)}, context.Canceled)
	out := buf.String()
	for _, want := range []string{
		boxTopLeft + boxHorizontal + " " + glyphWarn + " Argo CD ",
		"  " + glyphWarn + " Interrupted \u00b7 Argo CD",
		"    log      /logs/acme.jsonl",
		"    retry    nic deploy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "inspect") {
		t.Errorf("an interrupt has nothing to inspect:\n%s", out)
	}
}

// TestRendererWrite: output that doesn't come through the status stream is
// printed above the live panel, which is drawn again below it, instead of
// being erased by the next redraw.
func TestRendererWrite(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Infrastructure", 0))
	buf.Reset()

	_, _ = r.Write([]byte("! Received interrupt signal"))
	out := buf.String()
	erase, line, panel := strings.Index(out, eraseLive(2)), strings.Index(out, "! Received interrupt signal\n"), strings.LastIndex(out, boxTopLeft)
	if erase != 0 || line < 0 || panel < line {
		t.Errorf("want: erase the panel, the line, then the panel again; got %q", out)
	}
}

// TestPrettyRendererDeferredDraw: with a ticker, updates only change what the
// next tick draws, so a burst of them doesn't redraw the panel each time.
func TestPrettyRendererDeferredDraw(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Tick: time.Hour, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Infrastructure", 0))
	buf.Reset()
	for range 50 {
		r.Handle(updateAt(status.LevelInfo, "tofu event", time.Second))
	}
	r.Handle(updateAt(status.LevelSuccess, "Kind cluster created", time.Second))
	if buf.Len() != 0 {
		t.Errorf("updates were drawn before the tick:\n%q", buf.String())
	}
	r.mu.Lock()
	r.sink.tick(r.current, at(2*time.Second))
	r.mu.Unlock()
	if !strings.Contains(buf.String(), "Kind cluster created") {
		t.Errorf("the tick should draw the pending step:\n%s", buf.String())
	}
	r.Finish(nil, "")
}

// TestPrettyRendererResize: after the terminal is narrowed, the live panel's
// lines wrap onto more rows, and erasing it has to count those rows.
func TestPrettyRendererResize(t *testing.T) {
	cols := 60
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: cols, Live: true, Size: func() (int, int) { return cols, 40 }, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Infrastructure", 0)) // a title and a bottom border, 60 columns wide
	cols = 30
	buf.Reset()
	r.Handle(updateAt(status.LevelSuccess, "Kind cluster created", time.Second))
	// Each 60-column line now wraps onto two rows.
	if !strings.HasPrefix(buf.String(), eraseLive(2+2)) {
		t.Errorf("want the erase to cover 4 rows, got %q", buf.String())
	}
	for _, line := range strings.Split(stripANSI(buf.String()), "\n") {
		if width(line) > 30 {
			t.Errorf("redrawn line wider than the new width: %q", line)
		}
	}
}

// TestRendererSanitizesHeader: header fields come from the config before it
// is validated, so they can't drive the terminal either.
func TestRendererSanitizesHeader(t *testing.T) {
	h := testHeader
	h.Domain = "a\x1b[2Jb\nc"
	var buf bytes.Buffer
	NewPlain(&buf, Options{Now: fixedClock()}).Begin(h)
	if strings.Contains(buf.String(), "\x1b") || !strings.Contains(buf.String(), "domain: a[2Jb c\n") {
		t.Errorf("header not sanitized: %q", buf.String())
	}
}

// TestGitHubRendererEmptyPhase: a phase with no steps to show leaves no
// empty log group, only its result line.
func TestGitHubRendererEmptyPhase(t *testing.T) {
	var buf bytes.Buffer
	run(NewGitHub(&buf, Options{Now: fixedClock()}), []status.Update{
		phaseAt("Foundational services", 0),
		updateAt(status.LevelSuccess, "Root App-of-Apps applied", time.Second),
		phaseAt("Endpoint", 2*time.Second),
	}, nil)
	out := buf.String()
	if strings.Contains(out, "::group::Endpoint") || strings.Count(out, "::endgroup::") != 1 {
		t.Errorf("want one group, for Foundational services only:\n%s", out)
	}
	if !strings.Contains(out, "13:00:15 [ok]    Endpoint (13s)") {
		t.Errorf("the empty phase should still report its result:\n%s", out)
	}
}
