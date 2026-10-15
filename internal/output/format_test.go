package output

import "testing"

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{in: "auto", want: FormatAuto},
		{in: "pretty", want: FormatPretty},
		{in: "plain", want: FormatPlain},
		{in: "json", want: FormatJSON},
		{in: "github", want: FormatGitHub},
		{in: "", wantErr: true},
		{in: "JSON", wantErr: true},
		{in: "text", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseFormat(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFormat(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		format   Format
		terminal bool
		env      map[string]string
		goos     string
		want     Format
	}{
		{name: "auto on a terminal is pretty", format: FormatAuto, terminal: true, want: FormatPretty},
		{name: "auto on a pipe keeps json", format: FormatAuto, terminal: false, want: FormatJSON},
		{name: "auto in CI keeps json even in GitHub Actions", format: FormatAuto, env: map[string]string{"GITHUB_ACTIONS": "true"}, want: FormatJSON},
		{name: "NO_COLOR on a terminal is plain", format: FormatAuto, terminal: true, env: map[string]string{"NO_COLOR": "1"}, want: FormatPlain},
		{name: "dumb terminal is plain", format: FormatAuto, terminal: true, env: map[string]string{"TERM": "dumb"}, want: FormatPlain},
		{name: "explicit pretty on a pipe stays pretty", format: FormatPretty, want: FormatPretty},
		{name: "explicit json on a terminal stays json", format: FormatJSON, terminal: true, want: FormatJSON},
		{name: "explicit pretty ignores NO_COLOR", format: FormatPretty, terminal: true, env: map[string]string{"NO_COLOR": "1"}, want: FormatPretty},
		{name: "auto on a Windows terminal is plain", format: FormatAuto, terminal: true, goos: "windows", want: FormatPlain},
		{name: "explicit pretty on Windows stays pretty", format: FormatPretty, terminal: true, goos: "windows", want: FormatPretty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := goos
			t.Cleanup(func() { goos = old })
			goos = "linux"
			if tt.goos != "" {
				goos = tt.goos
			}
			getenv := func(k string) string { return tt.env[k] }
			if got := Resolve(tt.format, tt.terminal, getenv); got != tt.want {
				t.Errorf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}
