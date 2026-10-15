package output

import (
	"bytes"
	"strings"
	"testing"
)

var testSteps = []Step{
	{
		Title:    "Add this line to /etc/hosts",
		Required: true,
		Lines: []StepLine{
			{Value: "127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local"},
			{Note: "No wildcards: services added later need their hostname on this same line."},
		},
	},
	{
		Title: "Open the services",
		Lines: []StepLine{
			{Label: "Argo CD", Value: "https://argocd.nebari.local", URL: true},
			{Label: "Keycloak", Value: "https://keycloak.nebari.local", URL: true, After: "ready in a few minutes"},
		},
	},
}

func TestWriteNextStepsPlain(t *testing.T) {
	want := `
Next steps
  1. Add this line to /etc/hosts (required)
       127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local
       No wildcards: services added later need their hostname on this same line.

  2. Open the services
       Argo CD    https://argocd.nebari.local
       Keycloak   https://keycloak.nebari.local (ready in a few minutes)
`
	for _, f := range []Format{FormatPlain, FormatGitHub} {
		var buf bytes.Buffer
		WriteNextSteps(&buf, f, testSteps, Options{})
		if buf.String() != want {
			t.Errorf("%s: got\n%s\nwant\n%s", f, buf.String(), want)
		}
	}
}

func TestWriteNextStepsPretty(t *testing.T) {
	const termWidth = 80
	var buf bytes.Buffer
	WriteNextSteps(&buf, FormatPretty, testSteps, Options{Width: termWidth})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if lines[0] != "" {
		t.Errorf("a panel starts with a blank line, got %q", lines[0])
	}
	lines = lines[1:]
	if !strings.HasPrefix(lines[0], "  "+boxTopLeft+boxHorizontal+" Next steps ") {
		t.Errorf("title line = %q", lines[0])
	}
	for _, want := range []string{
		boxVertical + " 1  Add this line to /etc/hosts  required ",
		boxVertical + "    127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local ",
		boxVertical + " 2  Open the services ",
		boxVertical + "    Keycloak   https://keycloak.nebari.local  ready in a few minutes ",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, buf.String())
		}
	}
	for _, line := range lines {
		if width(line) != termWidth {
			t.Errorf("panel line is %d columns, want %d: %q", width(line), termWidth, line)
		}
	}
}

// TestWriteNextStepsPrettyKeepsLongValues: values are copied by the user, so
// one wider than the panel wraps instead of being cut.
func TestWriteNextStepsPrettyKeepsLongValues(t *testing.T) {
	host := "k8s-envoygat-envoyenv-0123456789-0123456789abcdef.elb.us-west-2.amazonaws.com"
	steps := []Step{{Title: "Create these DNS records", Lines: []StepLine{{Label: "Value", Value: host}}}}
	var buf bytes.Buffer
	WriteNextSteps(&buf, FormatPretty, steps, Options{Width: 60})

	var text strings.Builder
	for _, line := range strings.Split(buf.String(), "\n") {
		if width(line) > 60 {
			t.Errorf("line wider than the terminal: %q", line)
		}
		text.WriteString(strings.TrimSpace(strings.Trim(strings.TrimSpace(line), boxVertical)))
	}
	if !strings.Contains(text.String(), host) {
		t.Errorf("hostname lost:\n%s", buf.String())
	}
}

func TestWriteNextStepsNone(t *testing.T) {
	var buf bytes.Buffer
	WriteNextSteps(&buf, FormatPretty, nil, Options{})
	if buf.Len() != 0 {
		t.Errorf("no steps should print nothing, got %q", buf.String())
	}
}
