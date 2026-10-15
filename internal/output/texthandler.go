package output

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// TextHandler is the slog.Handler the CLI uses when the output format isn't
// JSON. It prints each record as one readable line, so the messages that
// reach slog directly (the CLI's final error, the interrupt notice) match the
// rest of the output:
//
//	✗ Command execution failed: deploy infrastructure: tofu apply: ...
//	! Received interrupt signal, initiating graceful shutdown signal=interrupt
//
// A top-level "error" attribute is joined to the message; other attributes
// follow as key=value pairs. In the github format, warnings and errors become
// workflow annotations instead.
type TextHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	paint  painter
	github bool
	attrs  []slog.Attr
	group  string
}

// NewTextHandler returns a TextHandler writing to w in the style of format f:
// coloured for pretty (24-bit with trueColor), annotations for github, plain
// text otherwise.
func NewTextHandler(w io.Writer, f Format, trueColor bool) *TextHandler {
	return &TextHandler{mu: &sync.Mutex{}, w: w, paint: newPainter(f == FormatPretty, trueColor), github: f == FormatGitHub}
}

// Enabled reports whether level is at least Info.
func (h *TextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

// Handle writes r as one line.
func (h *TextHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)

	var errText string
	var extra []string
	add := func(a slog.Attr) {
		if a.Key == "error" && errText == "" {
			errText = a.Value.String()
			return
		}
		extra = append(extra, a.Key+"="+a.Value.String())
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(qualify(h.group, a))
		return true
	})
	if errText != "" {
		b.WriteString(": " + errText)
	}
	if len(extra) > 0 {
		b.WriteString(" " + h.paint.paint(h.paint.pal.dim, strings.Join(extra, " ")))
	}
	line := b.String()
	switch {
	case h.github && r.Level >= slog.LevelError:
		line = "::error::" + escapeData(line)
	case h.github && r.Level >= slog.LevelWarn:
		line = "::warning::" + escapeData(line)
	case r.Level >= slog.LevelError:
		line = h.paint.icon(outcomeFail) + " " + line
	case r.Level >= slog.LevelWarn:
		line = h.paint.icon(outcomeWarn) + " " + line
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := fmt.Fprintln(h.w, line)
	return err
}

// WithAttrs returns a handler that adds attrs to every record, qualified by
// the groups opened so far.
func (h *TextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]slog.Attr{}, h.attrs...)
	for _, a := range attrs {
		c.attrs = append(c.attrs, qualify(h.group, a))
	}
	return &c
}

// WithGroup returns a handler that prefixes attribute keys with name.
func (h *TextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	if c.group != "" {
		name = c.group + "." + name
	}
	c.group = name
	return &c
}

// qualify prefixes a's key with group, slog's dotted convention for text.
func qualify(group string, a slog.Attr) slog.Attr {
	if group != "" {
		a.Key = group + "." + a.Key
	}
	return a
}
