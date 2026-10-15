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

// WriteBanners prints sections as the framed banners NIC has always printed
// in the JSON format, where tooling may read them. The other formats print
// the same guidance as numbered steps (see WriteNextSteps).
func WriteBanners(w io.Writer, sections []Section) {
	for _, s := range sections {
		writeBanner(w, s)
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
