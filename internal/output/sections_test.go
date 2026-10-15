package output

import (
	"bytes"
	"strings"
	"testing"
)

var testSection = Section{
	Title: "ARGO CD INSTALLED",
	Body: []string{
		"",
		"To access Argo CD:",
		"",
		"",
		"  kubectl port-forward svc/argocd-server -n argocd 8080:443",
		"",
	},
}

func TestWriteSections(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		want   string
	}{
		{
			name:   "plain drops framing blank lines and collapses runs",
			format: FormatPlain,
			want: "\nARGO CD INSTALLED\n" +
				"  To access Argo CD:\n" +
				"\n" +
				"    kubectl port-forward svc/argocd-server -n argocd 8080:443\n",
		},
		{
			name:   "github prints the same as plain",
			format: FormatGitHub,
			want: "\nARGO CD INSTALLED\n" +
				"  To access Argo CD:\n" +
				"\n" +
				"    kubectl port-forward svc/argocd-server -n argocd 8080:443\n",
		},
		{
			name:   "json keeps the framed banner",
			format: FormatJSON,
			want: "\n" + bannerRule + "\n  ARGO CD INSTALLED\n" + bannerRule + "\n" +
				"\n  To access Argo CD:\n\n\n    kubectl port-forward svc/argocd-server -n argocd 8080:443\n\n" +
				bannerRule + "\n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			WriteSections(&buf, tt.format, []Section{testSection}, Options{})
			if buf.String() != tt.want {
				t.Errorf("got\n%q\nwant\n%q", buf.String(), tt.want)
			}
		})
	}
}

func TestWriteSectionsPretty(t *testing.T) {
	const termWidth = 70
	var buf bytes.Buffer
	WriteSections(&buf, FormatPretty, []Section{testSection}, Options{Width: termWidth})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if lines[0] != "" {
		t.Errorf("a panel starts with a blank line, got %q", lines[0])
	}
	lines = lines[1:]
	if !strings.HasPrefix(lines[0], "  "+boxTopLeft+boxHorizontal+" ARGO CD INSTALLED ") {
		t.Errorf("title line = %q", lines[0])
	}
	if want := 2 + 3; len(lines) != want { // top, 3 body lines, bottom
		t.Errorf("got %d lines, want %d:\n%s", len(lines), want, buf.String())
	}
	for _, line := range lines {
		if width(line) != termWidth {
			t.Errorf("panel line is %d columns, want %d: %q", width(line), termWidth, line)
		}
	}
}

// TestWriteSectionsPrettyKeepsLongValues: a DNS record's value is copied by
// the user, so a line wider than the panel wraps instead of being cut.
func TestWriteSectionsPrettyKeepsLongValues(t *testing.T) {
	host := "k8s-envoygat-envoyenv-0123456789-0123456789abcdef.elb.us-west-2.amazonaws.com"
	var buf bytes.Buffer
	WriteSections(&buf, FormatPretty, []Section{{Title: "DNS", Body: []string{"CNAME  nebari.example.com  " + host}}}, Options{Width: 80})

	var text strings.Builder
	for _, line := range strings.Split(buf.String(), "\n") {
		if width(line) > 80 {
			t.Errorf("line wider than the terminal: %q", line)
		}
		text.WriteString(strings.Trim(line, " "+boxVertical))
	}
	if !strings.Contains(text.String(), host) {
		t.Errorf("hostname lost:\n%s", buf.String())
	}
}
