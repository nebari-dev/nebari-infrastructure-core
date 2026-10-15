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
// has steps to show, or a single line for a phase that has none, then the
// result line.
//
// On a terminal (live), the running phase's panel is drawn as soon as the
// phase starts and redrawn in place as updates arrive: its steps fill in,
// the newest informational message shows as an activity line, and the title
// carries a spinner and the elapsed time. When the phase ends the panel is
// drawn one last time with its outcome and duration, and stays in the
// scrollback. Without a terminal, each panel is printed once, when its phase
// ends.
type panels struct {
	w      io.Writer
	paint  painter
	live   bool
	width  int
	height int

	// size, when set, reads the terminal's current size, so the live panel
	// follows a resize.
	size func() (width, height int)
	// deferDraw leaves redrawing the live panel to the ticker: an update
	// then only changes what the next tick draws, which keeps a burst of
	// updates from flooding a slow terminal.
	deferDraw bool

	// The live panel: the widths of its lines on screen, the newest
	// informational message, the spinner frame, and whether the caller has
	// the terminal.
	liveWidths []int
	activity   string
	frame      int
	paused     bool

	// blank is true when the last line printed was empty, so a panel can
	// keep one empty line above it without doubling it. liveBlank is what
	// it was before the live panel was drawn, restored when it is cleared.
	blank, liveBlank bool
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
	return min(max(p.height-14, 3), 20)
}

func (p *panels) println(s string) {
	_, _ = fmt.Fprintln(p.w, s)
	p.blank = s == ""
}

// spaced prints an empty line unless the last line already was one.
func (p *panels) spaced() []string {
	if p.blank {
		return nil
	}
	return []string{""}
}

// header prints:
//
//	❱ nic deploy
//	  Deploying acme-nebari to aws · us-east-1          DRY RUN · no changes
//	  domain  nebari.example.com
//	  config  config.yaml · nic v0.15.0
//	  log     ~/.nic/logs/acme-nebari/20261014T124943Z-deploy.jsonl
func (p *panels) header(h Header) {
	pal := p.paint.pal
	p.println(p.paint.paint(pal.ok, glyphPrompt) + " " + h.Command)

	titleColor := pal.act
	if h.Danger {
		titleColor = pal.fail
	}
	lineWidth := p.innerWidth() + width(indent)
	title := indent + p.paint.paint(pal.bold+titleColor, h.Title)
	titleWidth := width(indent + h.Title)
	badgeWidth := 0
	if h.Badge != "" {
		badgeWidth = 2 + width(h.Badge)
	}
	if h.Target != "" {
		// A long target (a kubeconfig context can be a full ARN) is cut to
		// keep the title on one line, next to the badge.
		lead := 1 + width(h.TargetPrep) + 1
		t := truncate(h.Target, max(lineWidth-titleWidth-lead-badgeWidth, 8))
		title += " " + p.paint.paint(pal.dim, h.TargetPrep) + " " + p.dimSeparators(t)
		titleWidth += lead + width(t)
	}
	if h.Badge != "" {
		gap := max(lineWidth-titleWidth-width(h.Badge), 2)
		title += strings.Repeat(" ", gap) + p.paint.paint(pal.warn, h.Badge)
	}
	p.println(title)

	row := func(label, value string) {
		if value != "" {
			p.println(indent + p.paint.paint(pal.dim, fmt.Sprintf("%-6s", label)) + "  " + value)
		}
	}
	row("domain", h.Domain)
	config := h.ConfigFile
	if h.Version != "" {
		config += p.paint.paint(pal.dim, " · nic "+h.Version)
	}
	row("config", config)
	row("log", h.LogPath)
	p.println("")
}

// dimSeparators dims the " · " separators of a target such as "aws · us-east-1".
func (p *panels) dimSeparators(s string) string {
	parts := strings.Split(s, " · ")
	return strings.Join(parts, " "+p.paint.paint(p.paint.pal.dim, "·")+" ")
}

func (p *panels) phaseStart(ph *phase) {
	if p.live {
		p.activity = ""
		p.drawLive(ph, ph.start)
		return
	}
	// Without a terminal, say what is running, so a long phase isn't silent
	// until its panel is printed.
	p.println(indent + p.paint.paint(p.paint.pal.act, spinner[0]) + " " + p.paint.paint(p.paint.pal.name, ph.name))
}

func (p *panels) step(ph *phase, s step, at time.Time) {
	switch {
	case ph != nil && p.live:
		if s.outcome != outcomeInfo {
			// A result ends whatever the activity line was describing.
			p.activity = ""
		}
		if !p.deferDraw {
			p.drawLive(ph, at)
		}
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
	if !p.deferDraw {
		p.drawLive(ph, at)
	}
}

func (p *panels) tick(ph *phase, now time.Time) {
	if p.live && ph != nil {
		p.frame = (p.frame + 1) % len(spinner)
		p.drawLive(ph, now)
	}
}

func (p *panels) pause() {
	p.paused = true
	p.clearProgress()
}

func (p *panels) resume(ph *phase, now time.Time) {
	p.paused = false
	if p.live && ph != nil {
		p.drawLive(ph, now)
	}
}

// clearProgress erases the live panel, before anything else is printed.
func (p *panels) clearProgress() {
	if len(p.liveWidths) == 0 {
		return
	}
	p.resize()
	// A line wider than the terminal (it was narrowed since the panel was
	// drawn) wraps onto more rows, and all of them have to go.
	rows := 0
	for _, w := range p.liveWidths {
		rows += max(1, (w+p.width-1)/p.width)
	}
	// Up to the panel's first row, then clear to the end of the screen.
	_, _ = fmt.Fprintf(p.w, "\x1b[%dA\r\x1b[J", rows)
	p.liveWidths = nil
	p.blank = p.liveBlank
}

// resize picks up the terminal's current size, if it can be read.
func (p *panels) resize() {
	if p.size == nil {
		return
	}
	if w, h := p.size(); w > 0 {
		p.width, p.height = w, h
	}
}

// external prints b above the live panel and draws the panel again below it.
func (p *panels) external(b []byte, ph *phase, now time.Time) {
	p.clearProgress()
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	_, _ = p.w.Write(b)
	p.blank = len(b) == 1 || strings.HasSuffix(string(b), "\n\n")
	if p.live && ph != nil && !p.paused {
		p.drawLive(ph, now)
	}
}

// drawLive redraws the running phase's panel in place.
func (p *panels) drawLive(ph *phase, now time.Time) {
	if p.paused {
		return
	}
	p.clearProgress()
	p.resize()
	pal := p.paint.pal
	spin := p.paint.paint(pal.act, spinner[p.frame])
	var activity []string
	if p.activity != "" {
		activity = []string{spin + " " + p.paint.paint(pal.dim, truncate(p.activity, p.innerWidth()-6))}
	}
	p.liveBlank = p.blank
	lines := append(p.spaced(), p.panelLines(ph.name, spin, pal.act, formatDuration(now.Sub(ph.start)), ph.steps, activity, p.maxLiveLines())...)
	widths := make([]int, len(lines))
	for i, line := range lines {
		p.println(line)
		widths[i] = p.visible(line)
	}
	p.liveWidths = widths
}

func (p *panels) phaseEnd(ph *phase) {
	p.clearProgress()
	p.activity = ""
	pal := p.paint.pal
	dur := formatDuration(ph.duration())
	if len(ph.steps) == 0 {
		p.println(indent + p.row(p.paint.icon(ph.outcome)+" "+p.paint.paint(pal.name, ph.name), 2+width(ph.name), dur, p.innerWidth()))
		return
	}
	border := pal.border
	switch ph.outcome {
	case outcomeWarn:
		border = pal.warn
	case outcomeFail:
		border = pal.fail
	}
	for _, line := range append(p.spaced(), p.panelLines(ph.name, p.paint.icon(ph.outcome), border, dur, ph.steps, nil, 0)...) {
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
	var body []string
	for _, s := range steps {
		body = append(body, p.stepLines(s, p.innerWidth()-4)...)
	}
	body = append(body, extra...)
	if maxLines > 0 && len(body) > maxLines {
		hidden := len(body) - (maxLines - 1)
		body = append([]string{p.paint.paint(p.paint.pal.dim, fmt.Sprintf("… %d earlier", hidden))}, body[hidden:]...)
	}
	title := icon + " " + p.paint.paint(p.paint.pal.name, name)
	return p.box(title, 2+width(name), dur, border, body)
}

// box draws a rounded box with a title (and optional duration) set into its
// top border around body lines, which must fit innerWidth-4 columns.
func (p *panels) box(title string, titleWidth int, dur, border string, body []string) []string {
	boxWidth := p.innerWidth()
	content := boxWidth - 4 // "│ " and " │"
	right := p.paint.paint(border, boxHorizontal+boxTopRight)
	rightWidth := 2
	if dur != "" {
		right = " " + p.paint.paint(p.paint.pal.dim, dur) + " " + right
		rightWidth += 1 + width(dur) + 1
	}
	fill := max(boxWidth-2-(titleWidth+2)-rightWidth, 1)
	lines := []string{indent + p.paint.paint(border, boxTopLeft+boxHorizontal) + " " + title + " " +
		p.paint.paint(border, strings.Repeat(boxHorizontal, fill)) + right}
	for _, line := range body {
		lines = append(lines, indent+p.paint.paint(border, boxVertical)+" "+line+
			strings.Repeat(" ", max(content-p.visible(line), 0))+" "+p.paint.paint(border, boxVertical))
	}
	return append(lines, indent+p.paint.paint(border, boxBottomLeft+strings.Repeat(boxHorizontal, boxWidth-2)+boxBottomRight))
}

// result prints the result line and, after a failure, where to look next:
//
//	✓ Deployed nebari-local · https://nebari.local                      58s
//
//	✗ Deploy failed · Argo CD                                         5m28s
//
//	    inspect  kubectl get pods -n argocd
//	    log      ~/.nic/logs/nebari-local/20261014T124943Z-deploy.jsonl
//	    retry    nic deploy
func (p *panels) result(r resultLine) {
	pal := p.paint.pal
	p.println("")
	left := p.paint.icon(r.outcome) + " " + p.paint.paint(pal.name, r.text)
	leftWidth := 2 + width(r.text)
	dur := ""
	if r.elapsed > 0 {
		dur = formatDuration(r.elapsed)
	}
	var detail string
	if r.detail != "" {
		color := pal.act
		switch {
		case r.link:
		case r.outcome == outcomeFail:
			color = pal.fail
		case r.outcome == outcomeWarn:
			color = pal.warn
		}
		detail = p.paint.paint(color, r.detail)
	}
	switch {
	case detail == "":
	case leftWidth+3+width(r.detail)+2+width(dur) <= p.innerWidth():
		left += " " + p.paint.paint(pal.dim, "\u00b7") + " " + detail
		leftWidth += 3 + width(r.detail)
	default:
		// Too narrow for one line: the detail (a URL to copy) goes below,
		// whole, rather than being cut.
		p.println(indent + p.row(left, leftWidth, dur, p.innerWidth()))
		p.println(indent + "  " + detail)
		left = ""
	}
	if left != "" {
		p.println(indent + p.row(left, leftWidth, dur, p.innerWidth()))
	}
	if len(r.hints) > 0 {
		p.println("")
		for _, h := range r.hints {
			p.println(indent + "  " + p.paint.paint(pal.dim, fmt.Sprintf("%-7s", h[0])) + "  " + h[1])
		}
	}
}

// row lays out a painted left part of the given visible width and a
// right-aligned, dimmed duration.
func (p *panels) row(left string, leftWidth int, dur string, w int) string {
	if dur == "" {
		return left
	}
	gap := max(w-leftWidth-width(dur), 2)
	return left + strings.Repeat(" ", gap) + p.paint.paint(p.paint.pal.dim, dur)
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
			part = p.paint.paint(p.paint.pal.dim, part)
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
