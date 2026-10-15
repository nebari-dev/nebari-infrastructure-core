package output

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Header describes the command being rendered: what the renderer prints
// before the first phase, and the words it uses for the result line.
type Header struct {
	// Command is the command line as the user would type it, e.g. "nic deploy".
	Command string
	// Title is shown while the command runs, e.g. "Deploying acme-nebari".
	Title string
	// TargetPrep joins the title to the target, e.g. "to" or "on".
	TargetPrep string
	// Target is where the command runs: the cluster provider and, when it
	// has one, its region, location or kubeconfig context.
	Target string
	// Domain, ConfigFile and Version are shown under the title when set.
	Domain     string
	ConfigFile string
	Version    string
	// LogPath is the log file this run writes, if any.
	LogPath string
	// Badge is shown at the right of the title, e.g. a dry run's notice.
	Badge string
	// Danger draws the title in the failure colour, for destructive commands.
	Danger bool
	// Done is the result line on success, e.g. "Deployed acme-nebari".
	Done string
	// Failed is the result line on failure, e.g. "Deploy failed".
	Failed string
	// Retry is the command to run again after a failure, e.g. "nic deploy".
	Retry string
	// Inspect maps a phase name to a command that shows what went wrong in
	// it, offered when the command fails in that phase.
	Inspect map[string]string
}

// Options tune a Renderer.
type Options struct {
	// Verbose also shows informational updates and tool output (OpenTofu,
	// Helm, ...) under the step that produced them.
	Verbose bool
	// Live draws the running phase's panel as soon as the phase starts and
	// redraws it in place as updates arrive. Only set it when the writer is a
	// terminal.
	Live bool
	// DryRun shows informational updates (but not tool output) without
	// Verbose: a dry run's "Would create ..." lines are its whole result.
	DryRun bool
	// Color enables ANSI colour.
	Color bool
	// TrueColor uses 24-bit colours instead of the 16 standard ones. Only used
	// with Color.
	TrueColor bool
	// Width is the terminal width in columns. Zero means 80.
	Width int
	// Height is the terminal height in rows, which bounds how tall a live
	// panel grows. Zero picks a size that fits a common terminal.
	Height int
	// Size, when set, reads the terminal's current width and height, so the
	// live panel follows a resize. Only used with Live.
	Size func() (width, height int)
	// Tick redraws the live panel at this interval, advancing its spinner and
	// elapsed time through a long silent wait. With a tick, updates are only
	// drawn by it, at most once per tick. Zero disables it and draws each
	// update as it arrives. Only used with Live.
	Tick time.Duration
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
}

// step is one line of output inside a phase.
type step struct {
	outcome outcome
	text    string
	// detail is the error or extra explanation attached to the update.
	detail string
	// quiet marks informational updates and tool output, which are shown
	// only in verbose mode and drawn dimmed.
	quiet bool
}

// phase groups the steps between two status.Phase markers.
type phase struct {
	name       string
	start, end time.Time
	outcome    outcome
	steps      []step
}

func (p *phase) duration() time.Duration { return p.end.Sub(p.start) }

// sink draws what the Renderer tracks. Implementations are only ever called
// with the Renderer's lock held, so they need no locking of their own.
type sink interface {
	header(h Header)
	phaseStart(p *phase)
	// step reports a step as it happens, after it was added to p's steps.
	// p is nil for updates sent before the first phase.
	step(p *phase, s step, at time.Time)
	// progress shows a transient message for the running phase.
	progress(p *phase, message string, at time.Time)
	// tick refreshes the running phase's elapsed time.
	tick(p *phase, now time.Time)
	// clearProgress removes whatever is drawn live, before other output.
	clearProgress()
	// external writes output that didn't come through the renderer (a log
	// line, a client-go warning) without the live panel drawing over it.
	external(b []byte, p *phase, now time.Time)
	// pause removes what is drawn live and keeps it off until resume.
	pause()
	resume(p *phase, now time.Time)
	phaseEnd(p *phase)
	result(r resultLine)
}

// resultLine is what a command ends with: its outcome, a detail (the URL on
// success, the failing phase on failure), and where to look after a failure.
type resultLine struct {
	outcome outcome
	text    string
	detail  string
	// link marks detail as a link (the platform's URL) rather than the name
	// of the phase that failed.
	link    bool
	elapsed time.Duration
	// hints are label and command pairs, e.g. {"retry", "nic deploy"}.
	hints [][2]string
}

// Renderer turns status Updates into human-readable output, grouped by the
// phases pkg/nic announces with status.Phase. Its Handle method is a
// status.Handler.
//
// Handle runs on the status channel's consumer goroutine while Begin, Pause
// and Finish run on the command's goroutine, so every method takes the same
// lock: a Renderer is safe for concurrent use.
type Renderer struct {
	mu       sync.Mutex
	sink     sink
	opts     Options
	header   Header
	started  time.Time
	current  *phase
	finished bool
	// worst is the most severe outcome of the phases closed so far, so a run
	// that ends without an error but with warnings doesn't report success.
	worst outcome
	// stopTick ends the ticker goroutine, if one runs.
	stopTick chan struct{}
}

// NewPretty returns a Renderer that draws a panel per phase. Set opts.Live
// only when w is a terminal. With Live and Tick set it starts a goroutine
// that refreshes the live panel until Finish.
func NewPretty(w io.Writer, opts Options) *Renderer {
	opts = opts.withDefaults()
	r := &Renderer{sink: &panels{
		w: w, paint: newPainter(opts.Color, opts.TrueColor), live: opts.Live,
		width: opts.Width, height: opts.Height, size: opts.Size,
		deferDraw: opts.Live && opts.Tick > 0,
	}, opts: opts}
	if opts.Live && opts.Tick > 0 {
		r.stopTick = make(chan struct{})
		go r.tickEvery(opts.Tick)
	}
	return r
}

// tickEvery refreshes the live panel until Finish closes stopTick.
func (r *Renderer) tickEvery(d time.Duration) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-r.stopTick:
			return
		case <-t.C:
			r.mu.Lock()
			if !r.finished && r.current != nil {
				r.sink.tick(r.current, r.opts.Now())
			}
			r.mu.Unlock()
		}
	}
}

// NewPlain returns a Renderer that prints one line per step, without colour.
func NewPlain(w io.Writer, opts Options) *Renderer {
	return &Renderer{sink: &plain{w: w}, opts: opts.withDefaults()}
}

// NewGitHub returns a plain Renderer that also puts each phase in a GitHub
// Actions log group and raises warnings and failures as annotations. The
// groups are collapsed, so it shows informational updates as if verbose.
func NewGitHub(w io.Writer, opts Options) *Renderer {
	opts.Verbose = true
	return &Renderer{sink: &plain{w: w, github: true}, opts: opts.withDefaults()}
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Width <= 0 {
		o.Width = 80
	}
	return o
}

// Begin prints the header and starts the clock for the result line.
func (r *Renderer) Begin(h Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h = h.sanitized()
	r.header = h
	r.started = r.opts.Now()
	r.sink.header(h)
}

// Handle renders one status Update. It is a status.Handler.
func (r *Renderer) Handle(u status.Update) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	at := u.Timestamp
	if at.IsZero() {
		at = r.opts.Now()
	}

	if name, ok := status.PhaseName(u); ok {
		r.closePhase(at)
		r.current = &phase{name: name, start: at}
		r.sink.phaseStart(r.current)
		return
	}

	_, isTool := u.Metadata[status.MetadataKeyDetail]
	s := step{text: sanitize(u.Message), detail: sanitize(detailOf(u))}
	switch u.Level {
	case status.LevelError:
		s.outcome = outcomeFail
	case status.LevelWarning:
		s.outcome = outcomeWarn
	case status.LevelSuccess:
		s.outcome = outcomeOK
		s.quiet = isTool
	default:
		s.outcome = outcomeInfo
		s.quiet = isTool || !r.opts.DryRun
		r.sink.progress(r.current, s.text, at)
	}
	if s.quiet && !r.opts.Verbose {
		return
	}
	if r.current != nil {
		r.current.steps = append(r.current.steps, s)
		r.current.outcome = worse(r.current.outcome, s.outcome)
	}
	r.sink.step(r.current, s, at)
}

// Pause clears the live panel and keeps it off until Resume, so the caller
// can write to the terminal, for example to prompt for confirmation. Updates
// already queued on the status channel are still recorded, but none of them
// redraws over the prompt.
func (r *Renderer) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sink.pause()
}

// Resume draws the live panel again.
func (r *Renderer) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sink.resume(r.current, r.opts.Now())
}

// Write prints b, which didn't come through the status stream (a log record,
// a client-go warning), above the live panel instead of under it, where the
// next redraw would erase it. It is an io.Writer for the CLI's slog handler.
func (r *Renderer) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sink.external(b, r.current, r.opts.Now())
	return len(b), nil
}

// Finish closes the running phase and prints the result line. err is the
// command's error; the Renderer marks the running phase as failed and names
// it on the result line, but does not print err, since the CLI reports a
// returned error once on its own. link is shown on the result line of a
// successful run, e.g. the platform's URL. Call Finish after the status
// handler has been flushed. Later calls do nothing.
func (r *Renderer) Finish(err error, link string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	if r.stopTick != nil {
		close(r.stopTick)
	}
	now := r.opts.Now()
	interrupted := errors.Is(err, context.Canceled)
	failedIn := ""
	if err != nil && r.current != nil {
		// An interrupted phase didn't fail; it was stopped.
		mark := outcomeFail
		if interrupted {
			mark = outcomeWarn
		}
		r.current.outcome = worse(r.current.outcome, mark)
		failedIn = r.current.name
	}
	r.closePhase(now)
	r.sink.clearProgress()

	res := resultLine{elapsed: now.Sub(r.started)}
	switch {
	case err == nil && r.worst >= outcomeWarn:
		// Exit codes are the pkg/nic contract's to decide; the result line
		// only has to not call a run with warnings clean.
		res.outcome, res.text, res.detail, res.link = outcomeWarn, r.header.Done+" with warnings", link, true
	case err == nil:
		res.outcome, res.text, res.detail, res.link = outcomeOK, r.header.Done, link, true
	case interrupted:
		res.outcome, res.text = outcomeWarn, "Interrupted"
		res.detail = failedIn
		res.hints = r.hints("", r.header.Retry)
	default:
		res.outcome, res.text = outcomeFail, r.header.Failed
		res.detail = failedIn
		res.hints = r.hints(r.header.Inspect[failedIn], r.header.Retry)
	}
	r.sink.result(res)
}

// hints lists where to look after a run that didn't finish: a command that
// shows what went wrong, the log file, and the command to run again.
func (r *Renderer) hints(inspect, retry string) [][2]string {
	var hints [][2]string
	for _, h := range [][2]string{{"inspect", inspect}, {"log", r.header.LogPath}, {"retry", retry}} {
		if h[1] != "" {
			hints = append(hints, h)
		}
	}
	return hints
}

// closePhase ends the running phase, if any, at the given time.
func (r *Renderer) closePhase(at time.Time) {
	if r.current == nil {
		return
	}
	r.current.end = at
	if r.current.outcome == outcomeInfo {
		r.current.outcome = outcomeOK
	}
	r.worst = worse(r.worst, r.current.outcome)
	r.sink.clearProgress()
	r.sink.phaseEnd(r.current)
	r.current = nil
}

// detailOf returns the error attached to an update, the convention pkg/
// producers use for failures and warnings.
func detailOf(u status.Update) string {
	if v, ok := u.Metadata["error"]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// sanitized returns h with its text made safe for the terminal: the fields
// come from the config (project, domain, kubeconfig context) before it is
// validated.
func (h Header) sanitized() Header {
	for _, f := range []*string{&h.Command, &h.Title, &h.TargetPrep, &h.Target, &h.Domain, &h.ConfigFile,
		&h.Version, &h.LogPath, &h.Badge, &h.Done, &h.Failed, &h.Retry} {
		*f = oneLine(*f)
	}
	inspect := make(map[string]string, len(h.Inspect))
	for k, v := range h.Inspect {
		inspect[k] = oneLine(v)
	}
	h.Inspect = inspect
	return h
}
