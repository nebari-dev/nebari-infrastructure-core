package output

import (
	"fmt"
	"io"
	"strings"
)

// Step is one numbered item of the guidance printed after a command, such
// as the /etc/hosts line to add or the URLs to open.
type Step struct {
	// Title says what to do, e.g. "Open the services".
	Title string
	// Required marks a step the platform can't be reached without.
	Required bool
	// Lines are shown under the title.
	Lines []StepLine
}

// StepLine is one line under a step's title. Set one of Note, a Label with
// its Value, or a bare Value; a StepLine with none of them is a blank line.
type StepLine struct {
	// Label names the value, e.g. "Argo CD"; values line up after it.
	Label string
	// Value is the text to copy: a URL, a command, a hosts line.
	Value string
	// URL draws the value as a link.
	URL bool
	// After follows the value, dimmed, e.g. "ready in a few minutes".
	After string
	// Note is an explanation, dimmed.
	Note string
}

// WriteNextSteps prints steps in the style of format f: a numbered panel for
// pretty, and indented text otherwise. The JSON format keeps NIC's banners,
// see WriteBanners.
func WriteNextSteps(w io.Writer, f Format, steps []Step, opts Options) {
	if len(steps) == 0 {
		return
	}
	steps = sanitizedSteps(steps)
	if f == FormatPretty {
		p := &panels{w: w, paint: newPainter(opts.Color, opts.TrueColor), width: max(opts.Width, 40)}
		p.nextSteps(steps)
		return
	}
	writePlainSteps(w, steps)
}

// writePlainSteps prints the steps as indented text, without wrapping, so a
// value is never split when copied from a log.
//
//	Next steps
//	  1. Add this line to /etc/hosts (required)
//	       127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local
func writePlainSteps(w io.Writer, steps []Step) {
	var b strings.Builder
	b.WriteString("\nNext steps\n")
	for i, s := range steps {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  %d. %s", i+1, s.Title)
		if s.Required {
			b.WriteString(" (required)")
		}
		b.WriteString("\n")
		for _, l := range s.Lines {
			line := l.Value
			switch {
			case l.Note != "":
				line = l.Note
			case l.Label != "":
				line = fmt.Sprintf("%-10s %s", l.Label, l.Value)
			}
			if l.After != "" {
				line += " (" + l.After + ")"
			}
			if line == "" {
				b.WriteString("\n")
				continue
			}
			b.WriteString("       " + line + "\n")
		}
	}
	_, _ = io.WriteString(w, b.String())
}

// nextSteps draws the steps as one numbered panel:
//
//	╭─ Next steps ─────────────────────────────────────────╮
//	│ 1  Add this line to /etc/hosts  required             │
//	│    127.0.0.1 nebari.local argocd.nebari.local        │
//	│                                                      │
//	│ 2  Open the services                                 │
//	│    Argo CD    https://argocd.nebari.local            │
//	╰──────────────────────────────────────────────────────╯
func (p *panels) nextSteps(steps []Step) {
	pal := p.paint.pal
	content := p.innerWidth() - 4
	var body []string
	for i, s := range steps {
		if i > 0 {
			body = append(body, "")
		}
		num := fmt.Sprintf("%-2d", i+1)
		if s.Required {
			body = append(body, p.paint.paint(pal.warn, num)+" "+s.Title+"  "+p.paint.paint(pal.warn, "required"))
		} else {
			body = append(body, p.paint.paint(pal.act, num)+" "+s.Title)
		}
		for _, l := range s.Lines {
			body = append(body, p.stepLine(l, content)...)
		}
	}
	lines := append(p.spaced(), p.box(p.paint.paint(pal.name, "Next steps"), width("Next steps"), "", pal.border, body)...)
	for _, line := range lines {
		p.println(line)
	}
}

// stepLine renders one line of a step, indented under its title and wrapped
// rather than cut, since it carries values to copy.
func (p *panels) stepLine(l StepLine, w int) []string {
	pal := p.paint.pal
	const pad = "   "
	var out []string
	switch {
	case l.Note != "":
		for _, part := range wrap(l.Note, w-len(pad)) {
			out = append(out, pad+p.paint.paint(pal.dim, part))
		}
	default:
		label := ""
		if l.Label != "" {
			label = fmt.Sprintf("%-10s ", l.Label)
		}
		color := pal.name
		if l.URL {
			color = pal.act
		}
		parts := wrap(l.Value, w-len(pad)-len(label))
		for i, part := range parts {
			lead := label
			if i > 0 {
				lead = strings.Repeat(" ", len(label))
			}
			out = append(out, pad+lead+p.paint.paint(color, part))
		}
		if l.After != "" {
			last := len(out) - 1
			after := "  " + p.paint.paint(pal.dim, l.After)
			if p.visible(out[last]+after) <= w {
				out[last] += after
			} else {
				out = append(out, pad+strings.Repeat(" ", len(label))+p.paint.paint(pal.dim, l.After))
			}
		}
	}
	return out
}

// sanitizedSteps returns a copy of steps safe to print: values such as the
// domain and the load balancer's hostname come from the config and the
// cluster.
func sanitizedSteps(steps []Step) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		out[i] = Step{Title: oneLine(s.Title), Required: s.Required, Lines: make([]StepLine, len(s.Lines))}
		for j, l := range s.Lines {
			out[i].Lines[j] = StepLine{Label: oneLine(l.Label), Value: oneLine(l.Value), URL: l.URL, After: oneLine(l.After), Note: oneLine(l.Note)}
		}
	}
	return out
}
