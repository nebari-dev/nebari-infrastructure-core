package output

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{in: -time.Second, want: ""},
		{in: 0, want: "<1s"},
		{in: 420 * time.Millisecond, want: "<1s"},
		{in: 9900 * time.Millisecond, want: "9s"},
		{in: 28 * time.Second, want: "28s"},
		{in: 5 * time.Minute, want: "5m0s"},
		{in: 8*time.Minute + 41*time.Second, want: "8m41s"},
		{in: 65 * time.Minute, want: "1h5m"},
	}
	for _, tt := range tests {
		t.Run(tt.in.String(), func(t *testing.T) {
			if got := formatDuration(tt.in); got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{name: "fits", in: "deploy infrastructure", width: 40, want: []string{"deploy infrastructure"}},
		{name: "breaks on spaces", in: "failed pre-install: timed out waiting for the condition", width: 20,
			want: []string{"failed pre-install:", "timed out waiting", "for the condition"}},
		{name: "hard-breaks long words", in: "abcdefghij", width: 4, want: []string{"abcd", "efgh", "ij"}},
		{name: "keeps newlines", in: "one\ntwo", width: 10, want: []string{"one", "two"}},
		{name: "zero width", in: "x", width: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrap(tt.in, tt.width)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("wrap(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
			for _, line := range got {
				if width(line) > tt.width {
					t.Errorf("line %q is wider than %d", line, tt.width)
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{in: "short", width: 10, want: "short"},
		{in: "a longer message", width: 8, want: "a longe…"},
		{in: "anything", width: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := truncate(tt.in, tt.width); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
		})
	}
}
