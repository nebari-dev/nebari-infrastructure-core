// Package output renders NIC's status stream for people: one panel per phase
// on a terminal, plain lines for logs and dumb terminals, and collapsible
// groups in GitHub Actions. JSON output stays with pkg/nic's SlogHandler; this
// package only decides when to use it.
package output

import (
	"fmt"
	"strings"
)

// Format is the value of the --log-format flag.
type Format string

const (
	// FormatAuto picks pretty, plain or json from the terminal (see Resolve).
	FormatAuto Format = "auto"
	// FormatPretty draws a panel per phase, live while the phase runs, with colour.
	FormatPretty Format = "pretty"
	// FormatPlain prints one line per step, without colour or redrawing.
	FormatPlain Format = "plain"
	// FormatJSON prints NIC's structured slog records.
	FormatJSON Format = "json"
	// FormatGitHub is plain output with each phase in a GitHub Actions log
	// group and warnings and errors raised as annotations.
	FormatGitHub Format = "github"
)

// Formats lists every accepted --log-format value.
var Formats = []Format{FormatAuto, FormatPretty, FormatPlain, FormatJSON, FormatGitHub}

// ParseFormat validates a --log-format value.
func ParseFormat(s string) (Format, error) {
	for _, f := range Formats {
		if Format(s) == f {
			return f, nil
		}
	}
	names := make([]string, len(Formats))
	for i, f := range Formats {
		names[i] = string(f)
	}
	return "", fmt.Errorf("invalid --log-format %q: must be one of %s", s, strings.Join(names, ", "))
}

// Resolve turns FormatAuto into a concrete format and returns any other
// format unchanged. A terminal gets pretty output, or plain when NO_COLOR is
// set or TERM is dumb. Anything that isn't a terminal (a pipe, a file, CI)
// keeps JSON, so tooling that parses NIC's output keeps getting slog records.
func Resolve(f Format, isTerminal bool, getenv func(string) string) Format {
	if f != FormatAuto {
		return f
	}
	if !isTerminal {
		return FormatJSON
	}
	if getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" {
		return FormatPlain
	}
	return FormatPretty
}
