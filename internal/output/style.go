package output

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Glyphs, written as escapes so the source stays ASCII.
const (
	glyphOK     = "\u2713" // check mark
	glyphFail   = "\u2717" // ballot x
	glyphWarn   = "!"
	glyphInfo   = "\u00b7" // middle dot
	glyphPrompt = "\u2771" // heavy right-pointing angle bracket ornament

	boxTopLeft     = "\u256d"
	boxTopRight    = "\u256e"
	boxBottomLeft  = "\u2570"
	boxBottomRight = "\u256f"
	boxHorizontal  = "\u2500"
	boxVertical    = "\u2502"
)

// spinner is the running phase's icon, one frame per tick (braille dots).
var spinner = []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}

// sgrReset ends any SGR sequence.
const sgrReset = "\x1b[0m"

// palette holds the SGR sequence for each role in the pretty format.
type palette struct {
	ok, act, warn, fail, dim, border, name, bold string
}

var (
	// truePalette is the design palette, for terminals with 24-bit colour.
	truePalette = palette{
		ok:     "\x1b[38;2;75;196;72m",
		act:    "\x1b[38;2;14;206;200m",
		warn:   "\x1b[38;2;229;192;123m",
		fail:   "\x1b[38;2;242;109;109m",
		dim:    "\x1b[38;2;143;143;143m",
		border: "\x1b[38;2;107;107;107m",
		name:   "\x1b[1;38;2;237;237;237m",
		bold:   "\x1b[1m",
	}
	// basicPalette is its closest match in the 16 standard colours.
	basicPalette = palette{
		ok:     "\x1b[32m",
		act:    "\x1b[36m",
		warn:   "\x1b[33m",
		fail:   "\x1b[31m",
		dim:    "\x1b[90m",
		border: "\x1b[90m",
		name:   "\x1b[1;97m",
		bold:   "\x1b[1m",
	}
)

// outcome is the state of a step or a phase.
type outcome int

const (
	outcomeInfo outcome = iota
	outcomeOK
	outcomeWarn
	outcomeFail
)

// worse returns the more severe of two outcomes.
func worse(a, b outcome) outcome {
	return max(a, b)
}

// painter applies the palette's colours when enabled and is a no-op
// otherwise.
type painter struct {
	on  bool
	pal palette
}

func newPainter(color, trueColor bool) painter {
	if trueColor {
		return painter{on: color, pal: truePalette}
	}
	return painter{on: color, pal: basicPalette}
}

func (p painter) paint(sgr, s string) string {
	if !p.on || s == "" {
		return s
	}
	return sgr + s + sgrReset
}

func (p painter) icon(o outcome) string {
	switch o {
	case outcomeOK:
		return p.paint(p.pal.ok, glyphOK)
	case outcomeWarn:
		return p.paint(p.pal.warn, glyphWarn)
	case outcomeFail:
		return p.paint(p.pal.fail, glyphFail)
	default:
		return p.paint(p.pal.dim, glyphInfo)
	}
}

// width is the number of terminal columns s occupies, ignoring ANSI codes.
// Callers measure text before painting it.
func width(s string) int {
	return runewidth.StringWidth(s)
}

// truncate shortens s to at most w columns, ending it with an ellipsis when
// it was cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.Truncate(s, w, "\u2026")
}

// wrap breaks s into lines of at most w columns, splitting on spaces and
// hard-breaking words longer than a line.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var lines []string
	for para := range strings.SplitSeq(s, "\n") {
		line := ""
		for word := range strings.FieldsSeq(para) {
			for width(word) > w {
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				head := runewidth.Truncate(word, w, "")
				if head == "" {
					// A single character wider than the line; emit it anyway.
					_, size := utf8.DecodeRuneInString(word)
					head = word[:size]
				}
				lines = append(lines, head)
				word = word[len(head):]
			}
			switch {
			case word == "":
				// The whole word went out in hard-broken lines above.
			case line == "":
				line = word
			case width(line)+1+width(word) <= w:
				line += " " + word
			default:
				lines = append(lines, line)
				line = word
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// formatDuration renders a step or phase duration compactly: <1s, 27s,
// 5m28s, 1h5m.
func formatDuration(d time.Duration) string {
	switch {
	case d < 0:
		return ""
	case d < time.Second:
		return "<1s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// sanitize makes text from producers safe to lay out: tabs and carriage
// returns become spaces, and other control characters (including ESC, which
// would let a message drive the terminal) are dropped. Newlines stay, since
// warnings and failures wrap on them.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t' || r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
}
