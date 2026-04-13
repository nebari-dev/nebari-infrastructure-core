package renderer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ANSI color codes
const (
	colorReset     = "\033[0m"
	colorRed       = "\033[31m"
	colorGreen     = "\033[32m"
	colorYellow    = "\033[33m"
	colorBlue      = "\033[34m"
	colorCyan      = "\033[36m"
	colorDim       = "\033[2m"
	colorBold = "\033[1m"
)

// Column widths for the tabular layout
const (
	colOp       = 5  // " ✓  " or " ✗  "
	colResource = 38 // resource name with tree prefix
	colStatus   = 14 // "completed", "failed", etc.
	colDur      = 8  // "3m38s", "0.24s"
)

// symbols holds the Unicode/ASCII glyphs used in output.
type symbols struct {
	ok     string
	fail   string
	warn   string
	skip   string
	phase  string
	branch string
	last   string
	pipe   string
	hSep   string // header separator (thin line)
	dotSep string // detail containment separator (dotted)
}

var unicodeSymbols = symbols{
	ok: "✓", fail: "✗", warn: "⚠", skip: "─", phase: "▸",
	branch: "├─ ", last: "└─ ", pipe: "│  ",
	hSep: "───", dotSep: "┄",
}

var asciiSymbols = symbols{
	ok: "+", fail: "x", warn: "!", skip: "-", phase: ">",
	branch: "|-- ", last: "\\-- ", pipe: "|   ",
	hSep: "---", dotSep: ".",
}

// pendingStep holds a buffered step that hasn't been rendered yet.
// We buffer steps so we can decide whether to use ├─ (more steps follow)
// or └─ (last step in the phase).
type pendingStep struct {
	op      string
	opColor string
	detail  string
	status  string
	dur     string
	inPhase bool // whether this step was created inside a phase
}

// Pretty is a Renderer that outputs a Pulumi-style columnar, tree-structured display.
type Pretty struct {
	w       io.Writer
	verbose bool
	sym     symbols
	color   bool

	// state
	phase      string
	inDetail   bool         // currently inside a detail block (between dotted lines)
	pending    *pendingStep // step waiting to know if it's the last in its phase
	afterPhase bool         // true after EndPhase, used to print separator before out-of-phase steps
}

// NewPretty creates a Pretty renderer that writes to w.
func NewPretty(w io.Writer, verbose bool) *Pretty {
	return &Pretty{w: w, verbose: verbose, sym: unicodeSymbols, color: true}
}

// NewPlain creates a Plain renderer: ASCII-only symbols, no color.
func NewPlain(w io.Writer, verbose bool) *Pretty {
	return &Pretty{w: w, verbose: verbose, sym: asciiSymbols, color: false}
}

func (p *Pretty) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.w, format, args...)
}

func (p *Pretty) println(args ...any) {
	_, _ = fmt.Fprintln(p.w, args...)
}

func (p *Pretty) c(code, text string) string {
	if !p.color {
		return text
	}
	return code + text + colorReset
}

func (p *Pretty) formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	if d >= time.Minute {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// row prints a single columnar row: op | resource | status | duration
func (p *Pretty) row(op, opColor, resource, status, dur string) {
	opCell := p.padRight(" "+op, colOp)
	if opColor != "" {
		opCell = p.c(opColor, opCell)
	}

	resCell := p.padRight(resource, colResource)
	statCell := p.padRight(status, colStatus)

	if dur != "" {
		dur = p.c(colorDim, dur)
	}

	p.printf(" %s  %s  %s  %s\n", opCell, resCell, statCell, dur)
}

// separator prints a horizontal rule across all columns.
func (p *Pretty) separator() {
	p.printf(" %s  %s  %s  %s\n",
		p.c(colorDim, p.padRight(p.sym.hSep, colOp)),
		p.c(colorDim, strings.Repeat(p.sym.hSep, colResource/3)),
		p.c(colorDim, p.padRight(p.sym.hSep, colStatus)),
		p.c(colorDim, p.padRight(p.sym.hSep, colDur)),
	)
}

// detailSeparator prints a dotted line in the resource column under the pipe prefix.
func (p *Pretty) detailSeparator() {
	p.printf(" %s  %s\n",
		strings.Repeat(" ", colOp),
		p.c(colorDim, p.sym.pipe+strings.Repeat(p.sym.dotSep, colResource)),
	)
}

// closeDetailBlock closes an open detail block by printing the closing dotted separator.
func (p *Pretty) closeDetailBlock() {
	if !p.inDetail {
		return
	}
	p.inDetail = false
	p.detailSeparator()
}

// flushPending renders a buffered step. If isLast is true, the step is rendered
// with └─ (last step in phase); otherwise with ├─.
func (p *Pretty) flushPending(isLast bool) {
	if p.pending == nil {
		return
	}
	s := p.pending
	p.pending = nil

	resource := s.detail
	if s.inPhase {
		prefix := p.sym.branch
		if isLast {
			prefix = p.sym.last
		}
		resource = prefix + s.detail
	}

	p.row(s.op, s.opColor, resource, s.status, s.dur)
}

func (p *Pretty) padRight(s string, width int) string {
	vis := visibleLen(s)
	if vis >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vis)
}

func (p *Pretty) StartPhase(name string) {
	p.closeDetailBlock()
	p.flushPending(true) // any pending step from a previous phase is the last in that phase
	p.phase = name
	p.afterPhase = false
	p.println()
	p.row(p.sym.phase, colorCyan, p.c(colorBold, name), "", "")
}

func (p *Pretty) EndPhase(_ PhaseStatus, _ time.Duration) {
	p.closeDetailBlock()
	p.flushPending(true) // last step in the phase
	p.phase = ""
	p.afterPhase = true
}

func (p *Pretty) StartStep(_ string) {
	// Rendered on EndStep
}

func (p *Pretty) EndStep(status StepStatus, elapsed time.Duration, detail string) {
	p.closeDetailBlock()
	p.flushPending(false) // previous step is not the last — this new one follows

	op, opColor := p.stepOp(status)
	statusText := p.stepStatusText(status)
	dur := p.formatDuration(elapsed)

	// Steps outside a phase after a phase ended get a separator
	if p.phase == "" && p.afterPhase {
		p.separator()
		p.afterPhase = false
	}

	p.pending = &pendingStep{
		op:      op,
		opColor: opColor,
		detail:  detail,
		status:  statusText,
		dur:     dur,
		inPhase: p.phase != "",
	}
}

func (p *Pretty) stepOp(s StepStatus) (string, string) {
	switch s {
	case StepOK:
		return p.sym.ok, colorGreen
	case StepFailed:
		return p.sym.fail, colorRed
	case StepWarning:
		return p.sym.warn, colorYellow
	case StepSkipped:
		return p.sym.skip, colorDim
	default:
		return " ", ""
	}
}

func (p *Pretty) stepStatusText(s StepStatus) string {
	switch s {
	case StepOK:
		return p.c(colorGreen, "completed")
	case StepFailed:
		return p.c(colorRed, "failed")
	case StepWarning:
		return p.c(colorYellow, "warning")
	case StepSkipped:
		return p.c(colorDim, "skipped")
	default:
		return ""
	}
}

func (p *Pretty) Detail(line string) {
	if !p.verbose {
		return
	}

	// Flush any pending step before detail lines (it's not the last step
	// since detail lines follow it as part of the same phase context)
	p.flushPending(false)

	// Open a detail block if not already in one
	if !p.inDetail {
		p.inDetail = true
		p.detailSeparator()
	}

	// Detail lines are contained under the pipe, indented in the resource column
	p.printf(" %s  %s\n",
		strings.Repeat(" ", colOp),
		p.c(colorDim, p.sym.pipe+line),
	)
}

func (p *Pretty) Summary(items []SummaryItem) {
	if len(items) == 0 {
		return
	}

	// Flush any pending step before summary
	p.closeDetailBlock()
	p.flushPending(true)

	p.println()
	p.printf(" %s\n", p.c(colorBold+colorBlue, "Outputs:"))

	maxLabel := 0
	for _, item := range items {
		if len(item.Label) > maxLabel {
			maxLabel = len(item.Label)
		}
	}

	for _, item := range items {
		pad := maxLabel - len(item.Label)
		p.printf("     %s:%s  %s\n", item.Label, strings.Repeat(" ", pad), p.c(colorCyan, item.Value))
	}
	p.println()
}

func (p *Pretty) Error(err error, hint string) {
	p.closeDetailBlock()
	p.flushPending(false)
	p.println()
	p.printf("  %s  %s\n", p.c(colorBold+colorRed, "error:"), err.Error())
	if hint != "" {
		p.printf("         %s\n", p.c(colorDim, hint))
	}
}

func (p *Pretty) Warn(message string) {
	p.closeDetailBlock()
	p.flushPending(false)
	p.row(p.sym.warn, colorYellow, message, p.c(colorYellow, "warning"), "")
}

func (p *Pretty) Info(message string) {
	p.closeDetailBlock()
	p.flushPending(false)
	p.printf("  %s\n", message)
}

func (p *Pretty) Version(ver, commitHash, tofuVer string, clusterProviders, dnsProviders []string) {
	p.printf("%s\n", p.c(colorBold, "Nebari Infrastructure Core (NIC)"))
	p.printf("  Version:    %s\n", ver)
	p.printf("  Commit:     %s\n", commitHash)
	p.printf("  OpenTofu:   %s\n", tofuVer)
	p.printf("  Providers:  %s\n", strings.Join(clusterProviders, ", "))
	p.printf("  DNS:        %s\n", strings.Join(dnsProviders, ", "))
}

func (p *Pretty) Confirm(message string, details map[string]string, expected string) (bool, error) {
	p.println()
	p.printf(" %s  %s\n", p.c(colorBold+colorYellow, p.sym.warn), p.c(colorBold, message))
	p.println()

	maxKey := 0
	for k := range details {
		if len(k) > maxKey {
			maxKey = len(k)
		}
	}
	for k, v := range details {
		pad := maxKey - len(k)
		p.printf("     %s:%s  %s\n", k, strings.Repeat(" ", pad), v)
	}

	p.println()
	p.printf("  %s\n", p.c(colorRed, "This will permanently delete all resources and data."))
	p.printf("  %s\n", "This action cannot be undone.")
	p.println()
	p.printf("  Type %s to confirm: ", p.c(colorBold, "'"+expected+"'"))

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	p.println()
	return strings.TrimSpace(response) == expected, nil
}

func (p *Pretty) DetailWriter() io.Writer {
	return &lineWriter{r: p}
}

// visibleLen returns the visible length of a string, stripping ANSI escape codes.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\033' {
			inEsc = true
			continue
		}
		n++
	}
	return n
}
