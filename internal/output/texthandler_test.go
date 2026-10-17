package output

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stamps matches the time of day TextHandler starts a plain line with.
var stamps = regexp.MustCompile(`(?m)^\d\d:\d\d:\d\d `)

func TestTextHandler(t *testing.T) {
	tests := []struct {
		name string
		log  func(*slog.Logger)
		want string
	}{
		{
			name: "error joins the error attribute to the message",
			log: func(l *slog.Logger) {
				l.Error("Command execution failed", "error", errors.New("deploy infrastructure: tofu apply failed"))
			},
			want: glyphFail + " Command execution failed: deploy infrastructure: tofu apply failed\n",
		},
		{
			name: "warning keeps other attributes as key=value",
			log: func(l *slog.Logger) {
				l.Warn("Received interrupt signal, initiating graceful shutdown", "signal", "interrupt")
			},
			want: glyphWarn + " Received interrupt signal, initiating graceful shutdown signal=interrupt\n",
		},
		{
			name: "info has no icon",
			log:  func(l *slog.Logger) { l.Info("Shutdown complete") },
			want: "Shutdown complete\n",
		},
		{
			name: "debug is dropped",
			log:  func(l *slog.Logger) { l.Debug("noise") },
			want: "",
		},
		{
			name: "groups qualify later attributes only",
			log: func(l *slog.Logger) {
				l.With("provider", "aws").WithGroup("tofu").Info("Applied", "added", 1)
			},
			want: "Applied provider=aws tofu.added=1\n",
		},
		{
			name: "a grouped error attribute is not the record's error",
			log:  func(l *slog.Logger) { l.WithGroup("dns").Warn("Skipped", "error", "no zone") },
			want: glyphWarn + " Skipped dns.error=no zone\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(slog.New(NewTextHandler(&buf, FormatPlain, false)))
			if got := stamps.ReplaceAllString(buf.String(), ""); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTextHandlerGitHub(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(NewTextHandler(&buf, FormatGitHub, false))
	l.Error("Command execution failed", "error", errors.New("tofu apply: exit 1\n::add-mask::x"))
	l.Warn("Received interrupt signal")
	l.Info("Shutdown complete")

	want := "::error::Command execution failed: tofu apply: exit 1%0A::add-mask::x\n" +
		"::warning::Received interrupt signal\n" +
		"Shutdown complete\n"
	if got := stamps.ReplaceAllString(buf.String(), ""); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTextHandlerSanitizes(t *testing.T) {
	var buf bytes.Buffer
	slog.New(NewTextHandler(&buf, FormatPretty, false)).Error("Command execution failed\x1b[2J",
		"error", errors.New("push rejected: \x1b]52;c;aGk=\x07"), "remote", "\x1b[31morigin")
	if strings.Contains(buf.String(), "\x1b]") || strings.Contains(buf.String(), "\x1b[2J") || strings.Contains(buf.String(), "\x1b[31morigin") {
		t.Errorf("control sequences from the record reached the output: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "push rejected: ]52;c;aGk=") {
		t.Errorf("the error text should survive sanitizing: %q", buf.String())
	}
}

// TestTextHandlerTimestamps: plain and github lines start with the record's
// time of day; pretty and workflow commands don't.
func TestTextHandlerTimestamps(t *testing.T) {
	at := time.Date(2026, 10, 15, 13, 0, 38, 0, time.UTC)
	tests := []struct {
		format Format
		level  slog.Level
		want   string
	}{
		{format: FormatPlain, level: slog.LevelInfo, want: "13:00:38 Shutdown complete\n"},
		{format: FormatPlain, level: slog.LevelWarn, want: "13:00:38 " + glyphWarn + " Shutdown complete\n"},
		{format: FormatGitHub, level: slog.LevelInfo, want: "13:00:38 Shutdown complete\n"},
		{format: FormatGitHub, level: slog.LevelWarn, want: "::warning::Shutdown complete\n"},
		{format: FormatPretty, level: slog.LevelInfo, want: "Shutdown complete\n"},
	}
	for _, tt := range tests {
		t.Run(string(tt.format)+"/"+tt.level.String(), func(t *testing.T) {
			var buf bytes.Buffer
			if err := NewTextHandler(&buf, tt.format, false).Handle(context.Background(), slog.NewRecord(at, tt.level, "Shutdown complete", 0)); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}
