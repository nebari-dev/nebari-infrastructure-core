package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// maxPanelWidth caps how wide a panel grows on a wide terminal.
const maxPanelWidth = 100

// panels is the pretty format: a header, then a rounded box per phase that
// has steps to show, or a single line for a phase that has none.
//
// On a terminal (live), the running phase's panel is drawn as soon as the
// phase starts and redrawn in place as updates arrive: its steps fill in,
// the newest informational message shows as a dimmed activity line, and the
// header carries a running icon and the elapsed time. When the phase ends the
// panel is drawn one last time with its outcome and duration, and stays in the
// scrollback. Without a terminal, each panel is printed once, when its phase
// ends.
//
//	❯ nic deploy
//	  Deploying acme-nebari
//	  aws · config.yaml
//
//	  ✓ Configuration                                             0.1s
//	  ╭─ ✓ Infrastructure ─────────────────────────────────── 8m41s ─╮
//	  │ ✓ Infrastructure deployment completed                        │
//	  ╰──────────────────────────────────────────────────────────────╯
//	  ╭─ ● Argo CD ──────────────────────────────────────────── 12s ─╮
//	  │ ✓ Cluster is ready                                           │
//	  │ ● Installing Argo CD Helm chart                              │
//	  ╰──────────────────────────────────────────────────────────────╯
type panels struct {
	w      io.Writer
	paint  painter
	live   bool
	width  int
	height int

	// The live panel: how many lines of it are on screen, the newest
	// informational message, and whether the caller has the terminal.
	liveLines int
	activity  string
	paused    bool
}

// indent is the left margin of everything but the prompt line.
const indent = "  "

// innerWidth is the usable width inside the left margin.
func (p *panels) innerWidth() int {
	return min(p.width, maxPanelWidth) - width(indent)
}

// maxLiveLines is how many content lines a live panel shows, so that it
// always fits on screen: redrawing can't reach lines that have scrolled away.
func (p *panels) maxLiveLines() int {
	if p.height <= 0 {
		return 12
	}
	return min(max(p.height-12, 3), 20)
}

func (p *panels) println(s string) {
	_, _ = fmt.Fprintln(p.w, s)
}

func (p *panels) header(h Header) {
	p.println(p.paint.paint(sgrGreen, glyphPrompt) + " " + h.Command)
	p.println(indent + p.paint.paint(sgrBold+sgrCyan, h.Title))
	if len(h.Context) > 0 {
		p.println(indent + p.paint.paint(sgrDim, strings.Join(h.Context, " · ")))
	}
	if h.LogPath != "" {
		p.println(indent + p.paint.paint(sgrDim, "log "+h.LogPath))
	}
	p.println("")
}

func (p *panels) phaseStart(ph *phase) {
	if p.live {
		p.activity = ""
		p.drawLive(ph, ph.start)
		return
	}
	// Without a terminal, say what is running, so a long phase isn't silent
	// until its panel is printed.
	p.println(indent + p.paint.paint(sgrCyan, glyphRun) + " " + p.paint.paint(sgrBold, ph.name))
}

func (p *panels) step(ph *phase, s step, at time.Time) {
	switch {
	case ph != nil && p.live:
		if s.outcome != outcomeInfo {
			// A result ends whatever the activity line was describing.
			p.activity = ""
		}
		p.drawLive(ph, at)
	case ph != nil && s.outcome != outcomeWarn && s.outcome != outcomeFail:
		// Printed with the panel when the phase ends. Warnings and failures
		// also show as they happen, so a long phase doesn't hold them back.
	default:
		for _, line := range p.stepLines(s, p.innerWidth()) {
			p.println(indent + line)
		}
	}
}

func (p *panels) progress(ph *phase, message string, at time.Time) {
	if !p.live || ph == nil {
		return
	}
	p.activity = strings.ReplaceAll(message, "\n", " ")
	p.drawLive(ph, at)
}

func (p *panels) tick(ph *phase, now time.Time) {
	if p.live && ph != nil {
		p.drawLive(ph, now)
	}
}

func (p *panels) pause() {
	p.paused = true
	p.clearProgress()
}

func (p *panels) resume(ph *phase, now time.Time) {
	p.paused = false
	p.tick(ph, now)
}

// clearProgress erases the live panel, before anything else is printed.
func (p *panels) clearProgress() {
	if p.liveLines > 0 {
		// Up to the panel's first line, then clear to the end of the screen.
		_, _ = fmt.Fprintf(p.w, "\x1b[%dA\r\x1b[J", p.liveLines)
		p.liveLines = 0
	}
}

// drawLive redraws the running phase's panel in place.
func (p *panels) drawLive(ph *phase, now time.Time) {
	if p.paused {
		return
	}
	p.clearProgress()
	var activity []string
	if p.activity != "" {
		activity = []string{p.paint.paint(sgrCyan, glyphRun) + " " + p.paint.paint(sgrDim, truncate(p.activity, p.innerWidth()-6))}
	}
	lines := p.panelLines(ph.name, p.paint.paint(sgrCyan, glyphRun), sgrCyan, formatDuration(now.Sub(ph.start)), ph.steps, activity, p.maxLiveLines())
	for _, line := range lines {
		p.println(line)
	}
	p.liveLines = len(lines)
}

func (p *panels) phaseEnd(ph *phase) {
	p.clearProgress()
	p.activity = ""
	dur := formatDuration(ph.duration())
	if len(ph.steps) == 0 {
		p.println(indent + p.row(p.paint.icon(ph.outcome)+" "+p.paint.paint(sgrBold, ph.name), 2+width(ph.name), dur, p.innerWidth()))
		return
	}
	border := sgrDim
	switch ph.outcome {
	case outcomeWarn:
		border = sgrYellow
	case outcomeFail:
		border = sgrRed
	}
	for _, line := range p.panelLines(ph.name, p.paint.icon(ph.outcome), border, dur, ph.steps, nil, 0) {
		p.println(line)
	}
}

// panelLines draws a phase panel: a title with icon and duration, a line per
// step (wrapped warnings and failures take more), then any extra lines. With
// maxLines > 0 only the newest content lines are kept, behind a count of the
// ones left out.
//
//	╭─ ✓ Name ──────── 8m41s ─╮
//	│ ✓ step                  │
//	╰─────────────────────────╯
func (p *panels) panelLines(name, icon, border, dur string, steps []step, extra []string, maxLines int) []string {
	boxWidth := p.innerWidth()
	content := boxWidth - 4 // "│ " and " │"

	title := " " + icon + " " + p.paint.paint(sgrBold, name) + " "
	titleWidth := 1 + 1 + 1 + width(name) + 1
	right := p.paint.paint(border, boxHorizontal+boxTopRight)
	rightWidth := 2
	if dur != "" {
		right = " " + p.paint.paint(sgrDim, dur) + " " + right
		rightWidth += 1 + width(dur) + 1
	}
	fill := max(boxWidth-2-titleWidth-rightWidth, 1)
	lines := []string{indent + p.paint.paint(border, boxTopLeft+boxHorizontal) + title +
		p.paint.paint(border, strings.Repeat(boxHorizontal, fill)) + right}

	var body []string
	for _, s := range steps {
		body = append(body, p.stepLines(s, content)...)
	}
	body = append(body, extra...)
	if maxLines > 0 && len(body) > maxLines {
		hidden := len(body) - (maxLines - 1)
		body = append([]string{p.paint.paint(sgrDim, fmt.Sprintf("… %d earlier", hidden))}, body[hidden:]...)
	}
	for _, line := range body {
		lines = append(lines, indent+p.paint.paint(border, boxVertical)+" "+line+
			strings.Repeat(" ", max(content-p.visible(line), 0))+" "+p.paint.paint(border, boxVertical))
	}
	return append(lines, indent+p.paint.paint(border, boxBottomLeft+strings.Repeat(boxHorizontal, boxWidth-2)+boxBottomRight))
}

func (p *panels) result(o outcome, text string, elapsed time.Duration, logPath string) {
	p.println("")
	dur := ""
	if elapsed > 0 {
		dur = formatDuration(elapsed)
	}
	p.println(indent + p.row(p.paint.icon(o)+" "+p.paint.paint(sgrBold, text), 2+width(text), dur, p.innerWidth()))
	if logPath != "" {
		p.println(indent + p.paint.paint(sgrDim, "log "+logPath))
	}
}

// row lays out a painted left part of the given visible width and a
// right-aligned, dimmed duration.
func (p *panels) row(left string, leftWidth int, dur string, w int) string {
	if dur == "" {
		return left
	}
	gap := max(w-leftWidth-width(dur), 2)
	return left + strings.Repeat(" ", gap) + p.paint.paint(sgrDim, dur)
}

// stepLines renders a step to lines of at most w visible columns. Warnings
// and failures wrap, so their text is never cut; everything else is
// truncated to one line. The text after the icon is indented on wrap.
func (p *panels) stepLines(s step, w int) []string {
	icon := p.paint.icon(s.outcome)
	text := s.text
	if s.detail != "" {
		text += ": " + s.detail
	}
	body := w - 2 // icon and space
	var parts []string
	if s.outcome == outcomeWarn || s.outcome == outcomeFail {
		parts = wrap(text, body)
	} else {
		parts = []string{truncate(strings.ReplaceAll(text, "\n", " "), body)}
	}
	lines := make([]string, len(parts))
	for i, part := range parts {
		if s.quiet {
			part = p.paint.paint(sgrDim, part)
		}
		if i == 0 {
			lines[i] = icon + " " + part
		} else {
			lines[i] = "  " + part
		}
	}
	return lines
}

// visible is the width of a painted line: the painter only adds ANSI codes,
// so measure with them stripped.
func (p *panels) visible(s string) int {
	return width(stripANSI(s))
}

// stripANSI removes CSI escape sequences (colours, cursor movement): ESC [,
// parameter bytes, then one final byte in the range @ to ~.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
