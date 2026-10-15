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
	glyphRun    = "\u25cf" // black circle
	glyphPrompt = "\u276f" // heavy right-pointing angle

	boxTopLeft     = "\u256d"
	boxTopRight    = "\u256e"
	boxBottomLeft  = "\u2570"
	boxBottomRight = "\u256f"
	boxHorizontal  = "\u2500"
	boxVertical    = "\u2502"
)

// ANSI SGR sequences used by the pretty format.
const (
	sgrReset  = "\x1b[0m"
	sgrBold   = "\x1b[1m"
	sgrDim    = "\x1b[2m"
	sgrRed    = "\x1b[31m"
	sgrGreen  = "\x1b[32m"
	sgrYellow = "\x1b[33m"
	sgrCyan   = "\x1b[36m"

	// clearLine returns the cursor to column 0 and erases the line, used to
	// replace the live progress line.
	clearLine = "\r\x1b[2K"
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

// painter applies ANSI colour when enabled and is a no-op otherwise.
type painter bool

func (p painter) paint(sgr, s string) string {
	if !p || s == "" {
		return s
	}
	return sgr + s + sgrReset
}

func (p painter) icon(o outcome) string {
	switch o {
	case outcomeOK:
		return p.paint(sgrGreen, glyphOK)
	case outcomeWarn:
		return p.paint(sgrYellow, glyphWarn)
	case outcomeFail:
		return p.paint(sgrRed, glyphFail)
	default:
		return p.paint(sgrDim, glyphInfo)
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

// formatDuration renders a step or phase duration compactly: 0.4s, 28s,
// 8m41s, 1h05m.
func formatDuration(d time.Duration) string {
	switch {
	case d < 0:
		return ""
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
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
