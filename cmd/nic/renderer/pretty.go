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
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
)

// symbols holds the Unicode/ASCII glyphs used in output.
type symbols struct {
	ok      string
	fail    string
	warn    string
	skip    string
	bullet  string
	branch  string
	last    string
	pipe    string
	hline   string
	topLeft string
	botLeft string
	tee     string
}

var unicodeSymbols = symbols{
	ok: "✓", fail: "✗", warn: "!", skip: "-", bullet: "●",
	branch: "├─", last: "└─", pipe: "│", hline: "─",
	topLeft: "┌", botLeft: "└", tee: "├",
}

var asciiSymbols = symbols{
	ok: "[OK]", fail: "[FAIL]", warn: "[WARN]", skip: "[SKIP]", bullet: "*",
	branch: "|--", last: "\\--", pipe: "|", hline: "-",
	topLeft: "+", botLeft: "+", tee: "+",
}

// Pretty is a Renderer that outputs colored, Unicode tree-structured text.
type Pretty struct {
	w       io.Writer
	verbose bool
	sym     symbols
	color   bool
	phase   string
}

// NewPretty creates a Pretty renderer that writes to w.
// If verbose is true, Detail() lines are shown.
func NewPretty(w io.Writer, verbose bool) *Pretty {
	return &Pretty{w: w, verbose: verbose, sym: unicodeSymbols, color: true}
}

// NewPlain creates a Plain renderer: ASCII-only symbols, no color.
func NewPlain(w io.Writer, verbose bool) *Pretty {
	return &Pretty{w: w, verbose: verbose, sym: asciiSymbols, color: false}
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
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func (p *Pretty) rightAlign(left string, dur time.Duration, width int) string {
	durStr := p.formatDuration(dur)
	if durStr == "" {
		return left
	}
	// Calculate visible length (strip ANSI codes for padding)
	visLen := visibleLen(left)
	pad := width - visLen - len(durStr)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + p.c(colorDim, durStr)
}

func (p *Pretty) StartPhase(name string) {
	p.phase = name
	fmt.Fprintf(p.w, "\n  %s %s\n", p.c(colorCyan, p.sym.bullet), p.c(colorBold, name))
}

func (p *Pretty) EndPhase(status PhaseStatus, elapsed time.Duration) {
	// Phase end is implicit — the next StartPhase or final summary provides the visual break.
	// We only emit timing on the phase header after the fact if needed,
	// but the simpler approach is to let EndStep lines carry the detail.
	p.phase = ""
	_ = status
	_ = elapsed
}

func (p *Pretty) StartStep(_ string) {
	// Steps are rendered on EndStep with their final status, so StartStep is a no-op.
	// This avoids partial line rendering in non-interactive terminals.
}

func (p *Pretty) EndStep(status StepStatus, elapsed time.Duration, detail string) {
	icon := p.stepIcon(status)
	left := fmt.Sprintf("    %s %s %s", p.sym.branch, icon, detail)
	line := p.rightAlign(left, elapsed, 72)
	fmt.Fprintln(p.w, line)
}

func (p *Pretty) stepIcon(s StepStatus) string {
	switch s {
	case StepOK:
		return p.c(colorGreen, p.sym.ok)
	case StepFailed:
		return p.c(colorRed, p.sym.fail)
	case StepWarning:
		return p.c(colorYellow, p.sym.warn)
	case StepSkipped:
		return p.c(colorDim, p.sym.skip)
	default:
		return p.c(colorDim, p.sym.skip)
	}
}

func (p *Pretty) Detail(line string) {
	if !p.verbose {
		return
	}
	fmt.Fprintf(p.w, "    %s     %s\n", p.c(colorDim, p.sym.pipe), p.c(colorDim, line))
}

func (p *Pretty) Summary(items []SummaryItem) {
	if len(items) == 0 {
		return
	}

	// Find max label width for alignment
	maxLabel := 0
	for _, item := range items {
		if len(item.Label) > maxLabel {
			maxLabel = len(item.Label)
		}
	}

	// Calculate box width
	boxWidth := maxLabel + 4 // padding
	for _, item := range items {
		lineLen := maxLabel + 3 + len(item.Value) // "  Label  Value"
		if lineLen > boxWidth {
			boxWidth = lineLen
		}
	}
	boxWidth += 4 // border padding

	border := strings.Repeat(p.sym.hline, boxWidth)

	fmt.Fprintln(p.w)
	fmt.Fprintf(p.w, "  %s%s%s\n", p.sym.topLeft, border, p.sym.topLeft)
	for _, item := range items {
		pad := maxLabel - len(item.Label) + 2
		fmt.Fprintf(p.w, "  %s  %s%s%s  %s\n", p.sym.pipe, item.Label, strings.Repeat(" ", pad), item.Value, p.sym.pipe)
	}
	fmt.Fprintf(p.w, "  %s%s%s\n", p.sym.botLeft, border, p.sym.botLeft)
	fmt.Fprintln(p.w)
}

func (p *Pretty) Error(err error, hint string) {
	fmt.Fprintf(p.w, "\n      %s %s\n", p.c(colorRed, "Error:"), err.Error())
	if hint != "" {
		fmt.Fprintf(p.w, "      %s %s\n", p.c(colorDim, "Hint:"), hint)
	}
	fmt.Fprintln(p.w)
}

func (p *Pretty) Warn(message string) {
	fmt.Fprintf(p.w, "    %s %s\n", p.c(colorYellow, p.sym.warn), message)
}

func (p *Pretty) Info(message string) {
	fmt.Fprintf(p.w, "  %s\n", message)
}

func (p *Pretty) Version(ver, commitHash, tofuVer string, clusterProviders, dnsProviders []string) {
	fmt.Fprintf(p.w, "%s\n", p.c(colorBold, "Nebari Infrastructure Core (NIC)"))
	fmt.Fprintf(p.w, "Version:    %s\n", ver)
	fmt.Fprintf(p.w, "Commit:     %s\n", commitHash)
	fmt.Fprintf(p.w, "OpenTofu:   %s\n", tofuVer)
	fmt.Fprintf(p.w, "Providers:  %s\n", strings.Join(clusterProviders, ", "))
	fmt.Fprintf(p.w, "DNS:        %s\n", strings.Join(dnsProviders, ", "))
}

func (p *Pretty) Confirm(message string, details map[string]string, expected string) (bool, error) {
	fmt.Fprintf(p.w, "\n  %s  %s\n\n", p.c(colorYellow, "⚠"), p.c(colorBold, message))
	for k, v := range details {
		fmt.Fprintf(p.w, "    %s: %s\n", k, v)
	}
	fmt.Fprintf(p.w, "\n  %s\n", p.c(colorRed, "This will permanently delete all resources and data."))
	fmt.Fprintf(p.w, "  This action cannot be undone.\n")
	fmt.Fprintf(p.w, "\n  Type '%s' to confirm: ", expected)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	fmt.Fprintln(p.w)
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
