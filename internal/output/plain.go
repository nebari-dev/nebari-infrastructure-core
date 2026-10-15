package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// plain is the plain and github formats: one line per step, nothing
// redrawn, so it reads the same in a terminal, a log file or a CI job.
//
//	nic deploy: Deploying acme-nebari (aws, config.yaml)
//	[..]    Infrastructure
//	[ok]    Infrastructure: Infrastructure deployment completed
//	[ok]    Infrastructure (8m41s)
//
// With github set, each phase also becomes a collapsible log group and
// warnings and failures are raised as workflow annotations.
type plain struct {
	w      io.Writer
	github bool
}

func (p *plain) println(s string) {
	_, _ = fmt.Fprintln(p.w, s)
}

func tag(o outcome) string {
	switch o {
	case outcomeOK:
		return "[ok]  "
	case outcomeWarn:
		return "[warn]"
	case outcomeFail:
		return "[fail]"
	default:
		return "[info]"
	}
}

func (p *plain) header(h Header) {
	line := h.Command + ": " + h.Title
	if len(h.Context) > 0 {
		line += " (" + strings.Join(h.Context, ", ") + ")"
	}
	p.println(line)
	if h.LogPath != "" {
		p.println("log: " + h.LogPath)
	}
}

func (p *plain) phaseStart(ph *phase) {
	if p.github {
		p.println("::group::" + escapeData(ph.name))
		return
	}
	p.println("[..]    " + ph.name)
}

func (p *plain) step(ph *phase, s step, _ time.Time) {
	text := s.text
	if s.detail != "" {
		text += ": " + s.detail
	}
	prefix, title := "", "nic"
	if ph != nil {
		prefix, title = ph.name+": ", ph.name
	}

	// In GitHub Actions an annotation also shows in the log, highlighted, so
	// it replaces the plain line instead of repeating it.
	switch {
	case p.github && s.outcome == outcomeWarn:
		p.println("::warning title=" + escapeProperty(title) + "::" + escapeData(text))
	case p.github && s.outcome == outcomeFail:
		p.println("::error title=" + escapeProperty(title) + "::" + escapeData(text))
	case p.github:
		// A line the runner sees starting with "::" is a workflow command,
		// so keep multi-line text on one line.
		p.println(tag(s.outcome) + "  " + prefix + strings.ReplaceAll(text, "\n", " "))
	default:
		p.println(tag(s.outcome) + "  " + prefix + text)
	}
}

func (p *plain) progress(*phase, string, time.Time) {}

func (p *plain) tick(*phase, time.Time) {}

func (p *plain) clearProgress() {}

func (p *plain) pause() {}

func (p *plain) resume(*phase, time.Time) {}

func (p *plain) phaseEnd(ph *phase) {
	if p.github {
		p.println("::endgroup::")
	}
	p.println(tag(ph.outcome) + "  " + ph.name + " (" + formatDuration(ph.duration()) + ")")
}

func (p *plain) result(o outcome, text string, elapsed time.Duration, logPath string) {
	line := tag(o) + "  " + text
	if elapsed > 0 {
		line += " (" + formatDuration(elapsed) + ")"
	}
	p.println(line)
	if logPath != "" {
		p.println("log: " + logPath)
	}
}

// escapeData escapes a GitHub Actions workflow command message.
// https://github.com/actions/toolkit/blob/main/packages/core/src/command.ts
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeProperty escapes a workflow command property value, such as title.
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
