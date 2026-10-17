package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// plain is the plain and github formats: one line per step, nothing
// redrawn, so it reads the same in a terminal, a log file or a CI job. Each
// line starts with the time of day it reports on, so a saved log shows when
// things happened and how long the gaps between them were.
//
//	13:00:38 nic deploy: Deploying acme-nebari to aws · us-west-2
//	13:00:38 config: config.yaml (nic v0.15.0)
//	13:00:38 [..]    Infrastructure
//	13:09:19 [ok]    Infrastructure: Infrastructure deployment completed
//	13:09:19 [ok]    Infrastructure (8m41s)
//
// With github set, each phase also becomes a collapsible log group and
// warnings and failures are raised as workflow annotations. Those workflow
// command lines must start with "::", so they carry no time.
type plain struct {
	w      io.Writer
	github bool
	// grouped is true once the running phase's log group is open. It opens
	// at the phase's first step, so a phase with none (Endpoint, often)
	// leaves no empty group behind.
	grouped bool
}

// timeLayout is the time of day each plain line starts with. The time is
// shown in the zone it was recorded in: local time for a real run.
const timeLayout = "15:04:05"

// line prints s stamped with the time at.
func (p *plain) line(at time.Time, s string) {
	_, _ = fmt.Fprintln(p.w, at.Format(timeLayout)+" "+s)
}

// command prints a GitHub workflow command, which can't be prefixed.
func (p *plain) command(s string) {
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

func (p *plain) header(h Header, at time.Time) {
	line := h.Command + ": " + h.Title
	if h.Target != "" {
		line += " " + h.TargetPrep + " " + h.Target
	}
	if h.Badge != "" {
		line += " (" + h.Badge + ")"
	}
	p.line(at, line)
	config := h.ConfigFile
	if config != "" && h.Version != "" {
		config += " (nic " + h.Version + ")"
	}
	for _, f := range [][2]string{{"domain", h.Domain}, {"config", config}, {"log", h.LogPath}} {
		if f[1] != "" {
			p.line(at, f[0]+": "+f[1])
		}
	}
}

func (p *plain) phaseStart(ph *phase) {
	if p.github {
		p.grouped = false
		return
	}
	p.line(ph.start, "[..]    "+ph.name)
}

func (p *plain) step(ph *phase, s step, at time.Time) {
	text := s.text
	if s.detail != "" {
		text += ": " + s.detail
	}
	prefix, title := "", "nic"
	if ph != nil {
		prefix, title = ph.name+": ", ph.name
		if p.github && !p.grouped {
			p.command("::group::" + escapeData(ph.name))
			p.grouped = true
		}
	}

	// In GitHub Actions an annotation also shows in the log, highlighted, so
	// it replaces the plain line instead of repeating it.
	switch {
	case p.github && s.outcome == outcomeWarn:
		p.command("::warning title=" + escapeProperty(title) + "::" + escapeData(text))
	case p.github && s.outcome == outcomeFail:
		p.command("::error title=" + escapeProperty(title) + "::" + escapeData(text))
	case p.github:
		// A line the runner sees starting with "::" is a workflow command,
		// so keep multi-line text on one line.
		p.line(at, tag(s.outcome)+"  "+prefix+strings.ReplaceAll(text, "\n", " "))
	default:
		p.line(at, tag(s.outcome)+"  "+prefix+text)
	}
}

func (p *plain) progress(*phase, string, time.Time) {}

func (p *plain) tick(*phase, time.Time) {}

func (p *plain) clearProgress() {}

func (p *plain) external(b []byte, _ *phase, _ time.Time) {
	_, _ = p.w.Write(b)
}

func (p *plain) pause() {}

func (p *plain) resume(*phase, time.Time) {}

func (p *plain) phaseEnd(ph *phase) {
	if p.github && p.grouped {
		p.command("::endgroup::")
		p.grouped = false
	}
	p.line(ph.end, tag(ph.outcome)+"  "+ph.name+" ("+formatDuration(ph.duration())+")")
}

func (p *plain) result(r resultLine) {
	line := tag(r.outcome) + "  " + r.text
	if r.detail != "" {
		line += ": " + r.detail
	}
	if r.elapsed > 0 {
		line += " (" + formatDuration(r.elapsed) + ")"
	}
	p.line(r.at, line)
	for _, h := range r.hints {
		p.line(r.at, h[0]+": "+h[1])
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
