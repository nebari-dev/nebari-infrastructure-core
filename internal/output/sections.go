package output

import (
	"io"
	"strings"
)

// Section is a block of guidance printed after a command, such as the DNS
// records to create after a deploy without a DNS provider.
type Section struct {
	// Title names the section, e.g. "DNS CONFIGURATION REQUIRED".
	Title string
	// Body holds the section's lines. Lines keep their indentation relative
	// to each other, and an empty string is a blank line. A leading and a
	// trailing blank line frame the body in the JSON format's banner and are
	// dropped elsewhere.
	Body []string
}

// bannerRule is the double rule framing a section in the JSON format.
var bannerRule = strings.Repeat("═", 79)

// WriteSections prints sections in the style of format f: JSON keeps the
// framed banners NIC has always printed, pretty draws a panel per section,
// and plain and github print indented text.
func WriteSections(w io.Writer, f Format, sections []Section, opts Options) {
	for _, s := range sections {
		switch f {
		case FormatJSON:
			writeBanner(w, s)
		case FormatPretty:
			p := &panels{w: w, paint: painter(opts.Color), width: max(opts.Width, 40)}
			p.section(s)
		default:
			writePlainSection(w, s)
		}
	}
}

// writeBanner is the original post-deploy banner: a title between two double
// rules, the body indented by two spaces, and a closing rule.
func writeBanner(w io.Writer, s Section) {
	var b strings.Builder
	b.WriteString("\n" + bannerRule + "\n  " + s.Title + "\n" + bannerRule + "\n")
	for _, line := range s.Body {
		if line != "" {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString(bannerRule + "\n\n")
	_, _ = io.WriteString(w, b.String())
}

func writePlainSection(w io.Writer, s Section) {
	var b strings.Builder
	b.WriteString("\n" + s.Title + "\n")
	for _, line := range trimBlank(s.Body) {
		if line != "" {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	_, _ = io.WriteString(w, b.String())
}

// section draws s as a panel titled with its name.
func (p *panels) section(s Section) {
	body := trimBlank(s.Body)
	boxWidth := p.innerWidth()
	content := boxWidth - 4
	title := " " + p.paint.paint(sgrBold, s.Title) + " "
	fill := max(boxWidth-2-(width(s.Title)+2)-1, 1)

	p.println("")
	p.println(indent + p.paint.paint(sgrDim, boxTopLeft+boxHorizontal) + title +
		p.paint.paint(sgrDim, strings.Repeat(boxHorizontal, fill)+boxTopRight))
	for _, line := range body {
		// Wrap rather than cut: these lines carry values to copy, such as a
		// DNS record's target.
		for _, part := range wrapIndented(line, content) {
			p.println(indent + p.paint.paint(sgrDim, boxVertical) + " " + part +
				strings.Repeat(" ", max(content-width(part), 0)) + " " + p.paint.paint(sgrDim, boxVertical))
		}
	}
	p.println(indent + p.paint.paint(sgrDim, boxBottomLeft+strings.Repeat(boxHorizontal, boxWidth-2)+boxBottomRight))
}

// trimBlank drops leading and trailing blank lines and collapses runs of
// blank lines into one.
func trimBlank(lines []string) []string {
	var out []string
	for _, line := range lines {
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// wrapIndented wraps line to w columns, keeping its leading indentation on
// every continuation line.
func wrapIndented(line string, w int) []string {
	if width(line) <= w {
		return []string{line}
	}
	trimmed := strings.TrimLeft(line, " ")
	lead := line[:len(line)-len(trimmed)]
	if width(lead) >= w/2 {
		lead = ""
	}
	parts := wrap(trimmed, w-width(lead))
	for i := range parts {
		parts[i] = lead + parts[i]
	}
	return parts
}
