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
	Command: "nic deploy",
	Title:   "Deploying acme",
	Context: []string{"aws", "config.yaml"},
	LogPath: "/logs/acme.jsonl",
	Done:    "Deployed acme",
	Failed:  "Deploy failed",
}

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
	r.Finish(err)
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
			want: `nic deploy: Deploying acme (aws, config.yaml)
log: /logs/acme.jsonl
[..]    Configuration
[ok]    Configuration (0.1s)
[..]    Infrastructure
[ok]    Infrastructure: Infrastructure deployment completed
[ok]    Infrastructure (9.9s)
[..]    Argo CD
[warn]  Argo CD: Failed to install Argo CD: boom
[warn]  Argo CD (5.0s)
[ok]    Deployed acme (15s)
log: /logs/acme.jsonl
`,
		},
		{
			name:    "plain verbose shows info and tool output",
			new:     func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			verbose: true,
			want: `nic deploy: Deploying acme (aws, config.yaml)
log: /logs/acme.jsonl
[..]    Configuration
[info]  Configuration: Starting deployment
[ok]    Configuration (0.1s)
[..]    Infrastructure
[info]  Infrastructure: Plan: 1 to add, 0 to change, 1 to destroy.
[ok]    Infrastructure: Infrastructure deployment completed
[ok]    Infrastructure (9.9s)
[..]    Argo CD
[warn]  Argo CD: Failed to install Argo CD: boom
[warn]  Argo CD (5.0s)
[ok]    Deployed acme (15s)
log: /logs/acme.jsonl
`,
		},
		{
			name: "failure marks the running phase and the result",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			err:  errors.New("install argo cd: timed out"),
			want: `nic deploy: Deploying acme (aws, config.yaml)
log: /logs/acme.jsonl
[..]    Configuration
[ok]    Configuration (0.1s)
[..]    Infrastructure
[ok]    Infrastructure: Infrastructure deployment completed
[ok]    Infrastructure (9.9s)
[..]    Argo CD
[warn]  Argo CD: Failed to install Argo CD: boom
[fail]  Argo CD (5.0s)
[fail]  Deploy failed after 15s
log: /logs/acme.jsonl
`,
		},
		{
			name: "interrupt",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewPlain(b, o) },
			err:  context.Canceled,
			want: `nic deploy: Deploying acme (aws, config.yaml)
log: /logs/acme.jsonl
[..]    Configuration
[ok]    Configuration (0.1s)
[..]    Infrastructure
[ok]    Infrastructure: Infrastructure deployment completed
[ok]    Infrastructure (9.9s)
[..]    Argo CD
[warn]  Argo CD: Failed to install Argo CD: boom
[fail]  Argo CD (5.0s)
[warn]  Interrupted after 15s
log: /logs/acme.jsonl
`,
		},
		{
			name: "github groups phases and annotates warnings",
			new:  func(b *bytes.Buffer, o Options) *Renderer { return NewGitHub(b, o) },
			want: `nic deploy: Deploying acme (aws, config.yaml)
log: /logs/acme.jsonl
::group::Configuration
[info]  Configuration: Starting deployment
::endgroup::
[ok]    Configuration (0.1s)
::group::Infrastructure
[info]  Infrastructure: Plan: 1 to add, 0 to change, 1 to destroy.
[ok]    Infrastructure: Infrastructure deployment completed
::endgroup::
[ok]    Infrastructure (9.9s)
::group::Argo CD
::warning title=Argo CD::Failed to install Argo CD: boom
::endgroup::
[warn]  Argo CD (5.0s)
[ok]    Deployed acme (15s)
log: /logs/acme.jsonl
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
		"  Deploying acme",
		"  aws \u00b7 config.yaml",
		"  log /logs/acme.jsonl",
		"  " + glyphOK + " Configuration",
		boxTopLeft + boxHorizontal + " " + glyphOK + " Infrastructure ",
		boxVertical + " " + glyphOK + " Infrastructure deployment completed",
		boxTopLeft + boxHorizontal + " " + glyphWarn + " Argo CD ",
		glyphWarn + " Failed to install Argo CD: boom",
		"  " + glyphOK + " Deployed acme",
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

func TestPrettyRendererLiveLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Infrastructure", 0))
	r.Handle(updateAt(status.LevelProgress, "Creating node group", 4*time.Second))

	live := buf.String()
	if !strings.Contains(live, clearLine) || !strings.Contains(live, "Creating node group") || !strings.Contains(live, "4.0s") {
		t.Fatalf("expected a live progress line, got %q", live)
	}

	buf.Reset()
	r.Pause()
	if buf.String() != clearLine {
		t.Errorf("Pause() wrote %q, want the line cleared", buf.String())
	}

	buf.Reset()
	r.Pause()
	if buf.Len() != 0 {
		t.Errorf("a second Pause() wrote %q, want nothing", buf.String())
	}
}

func TestRendererFinishIsFinal(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf, Options{Now: fixedClock()})
	run(r, nil, nil)
	before := buf.String()

	r.Handle(phaseAt("Late", time.Second))
	r.Finish(errors.New("late"))
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
	r.Finish(nil)
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
	r.Finish(nil)

	if got := strings.Count(buf.String(), "[ok]    Infrastructure: producer"); got != 100 {
		t.Errorf("rendered %d steps, want 100:\n%s", got, buf.String())
	}
}

func TestRendererPauseKeepsTheLiveLineOff(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Live: true, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Configuration", 0))
	r.Handle(updateAt(status.LevelInfo, "Starting destruction", time.Second))

	r.Pause()
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
		t.Errorf("the live line should return after Resume, got %q", buf.String())
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

// TestPrettyRendererWithoutLiveLine: off a terminal there is no live line,
// so the running phase is named when it starts, and warnings show as they
// happen instead of only in the panel at the end of the phase.
func TestPrettyRendererWithoutLiveLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewPretty(&buf, Options{Width: 60, Now: fixedClock()})
	r.Begin(testHeader)
	r.Handle(phaseAt("Argo CD", 0))
	r.Handle(updateAt(status.LevelWarning, "Failed to install Argo CD", time.Second))

	out := buf.String()
	for _, want := range []string{glyphRun + " Argo CD", glyphWarn + " Failed to install Argo CD"} {
		if !strings.Contains(out, want) {
			t.Errorf("before the phase ends, output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, boxTopLeft) {
		t.Errorf("the panel should wait for the phase to end:\n%s", out)
	}
}
