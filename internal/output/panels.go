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
// has steps to show, or a single line for a phase that has none, and a live
// progress line for the running phase.
//
//	❯ nic deploy
//	  Deploying acme-nebari
//	  aws · config.yaml
//
//	  ✓ Configuration                                             0.1s
//	  ╭─ ✓ Infrastructure ─────────────────────────────────── 8m41s ─╮
//	  │ ✓ Infrastructure deployment completed                        │
//	  ╰──────────────────────────────────────────────────────────────╯
type panels struct {
	w     io.Writer
	paint painter
	live  bool
	width int
	// progressShown is true while the live line is on screen.
	progressShown bool
}

// indent is the left margin of everything but the prompt line.
const indent = "  "

// innerWidth is the usable width inside the left margin.
func (p *panels) innerWidth() int {
	return min(p.width, maxPanelWidth) - width(indent)
}

func (p *panels) println(s string) {
	_, _ = fmt.Fprintln(p.w, s)
}

func (p *panels) header(h Header) {
	p.println(p.paint.paint(sgrGreen, glyphPrompt) + " " + h.Command)
	p.println(indent + p.paint.paint(sgrBold+sgrCyan, h.Title))
	if len(h.Context) > 0 {
		p.println(indent + p.paint.paint(sgrDim, strings.Join(h.Context, " \u00b7 ")))
	}
	if h.LogPath != "" {
		p.println(indent + p.paint.paint(sgrDim, "log "+h.LogPath))
	}
	p.println("")
}

func (p *panels) phaseStart(ph *phase) {
	if !p.live {
		// Without a live line, say what is running, so a long phase isn't
		// silent until its panel is drawn.
		p.println(indent + p.paint.paint(sgrCyan, glyphRun) + " " + p.paint.paint(sgrBold, ph.name))
	}
}

func (p *panels) step(ph *phase, s step) {
	// Steps inside a phase are drawn with its panel when it ends. Warnings
	// and failures also show as they happen, so they aren't held back for
	// the length of a long phase.
	if ph != nil && s.outcome != outcomeWarn && s.outcome != outcomeFail {
		return
	}
	p.clearProgress()
	for _, line := range p.stepLines(s, p.innerWidth()) {
		p.println(indent + line)
	}
}

func (p *panels) progress(ph *phase, message string, at time.Time) {
	if !p.live || ph == nil {
		return
	}
	// ● Name  message                 elapsed
	elapsed := formatDuration(at.Sub(ph.start))
	room := p.innerWidth() - 2 - 2 - width(elapsed) // icon and space, gap before elapsed
	message = strings.ReplaceAll(message, "\n", " ")
	name := truncate(ph.name, room)
	used := width(name)
	msg := ""
	if left := room - used - 2; left > 0 && message != "" {
		msg = truncate(message, left)
		used += 2 + width(msg)
	}
	line := indent + p.paint.paint(sgrCyan, glyphRun) + " " + p.paint.paint(sgrBold, name)
	if msg != "" {
		line += "  " + p.paint.paint(sgrDim, msg)
	}
	line += strings.Repeat(" ", max(room-used, 0)+2) + p.paint.paint(sgrDim, elapsed)
	_, _ = fmt.Fprint(p.w, clearLine+line)
	p.progressShown = true
}

func (p *panels) clearProgress() {
	if p.progressShown {
		_, _ = fmt.Fprint(p.w, clearLine)
		p.progressShown = false
	}
}

func (p *panels) phaseEnd(ph *phase) {
	dur := formatDuration(ph.duration())
	if len(ph.steps) == 0 {
		p.println(indent + p.row(p.paint.icon(ph.outcome)+" "+p.paint.paint(sgrBold, ph.name), 2+width(ph.name), dur, p.innerWidth()))
		return
	}

	boxWidth := p.innerWidth()
	content := boxWidth - 4 // "│ " and " │"
	border := sgrDim
	switch ph.outcome {
	case outcomeWarn:
		border = sgrYellow
	case outcomeFail:
		border = sgrRed
	}

	// ╭─ ✓ Name ──────── 8m41s ─╮
	title := " " + p.paint.icon(ph.outcome) + " " + p.paint.paint(sgrBold, ph.name) + " "
	titleWidth := 1 + 1 + 1 + width(ph.name) + 1
	right := boxHorizontal + boxTopRight
	rightWidth := 2
	if dur != "" {
		right = " " + p.paint.paint(sgrDim, dur) + " " + p.paint.paint(border, boxHorizontal+boxTopRight)
		rightWidth = 1 + width(dur) + 1 + 2
	} else {
		right = p.paint.paint(border, right)
	}
	fill := max(boxWidth-2-titleWidth-rightWidth, 1)
	p.println(indent + p.paint.paint(border, boxTopLeft+boxHorizontal) + title +
		p.paint.paint(border, strings.Repeat(boxHorizontal, fill)) + right)

	for _, s := range ph.steps {
		for _, line := range p.stepLines(s, content) {
			p.println(indent + p.paint.paint(border, boxVertical) + " " + line +
				strings.Repeat(" ", max(content-p.visible(line), 0)) + " " + p.paint.paint(border, boxVertical))
		}
	}
	p.println(indent + p.paint.paint(border, boxBottomLeft+strings.Repeat(boxHorizontal, boxWidth-2)+boxBottomRight))
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

// stripANSI removes SGR escape sequences.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
