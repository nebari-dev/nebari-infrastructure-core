package output

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
)

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
			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
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
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
