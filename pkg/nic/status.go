package nic

import (
	"context"
	"log/slog"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// statusLogHandler returns a status.Handler that logs each Update as a slog
// record on the client's logger. The Update's Message becomes the slog msg
// and its Level maps to a slog level. Resource, Action, and Metadata flow
// through as attrs.
func (c *Client) statusLogHandler() status.Handler {
	logger := c.logger
	return func(update status.Update) {
		attrs := make([]slog.Attr, 0, 2+len(update.Metadata))
		if update.Resource != "" {
			attrs = append(attrs, slog.String("resource", update.Resource))
		}
		if update.Action != "" {
			attrs = append(attrs, slog.String("action", update.Action))
		}
		for key, value := range update.Metadata {
			attrs = append(attrs, slog.Any(key, value))
		}
		// Handler runs detached from the caller's ctx (in the goroutine
		// started by status.StartHandler), so there's nothing to propagate.
		logger.LogAttrs(context.Background(), mapSlogLevel(update.Level), update.Message, attrs...)
	}
}

func mapSlogLevel(l status.Level) slog.Level {
	switch l {
	case status.LevelWarning:
		return slog.LevelWarn
	case status.LevelError:
		return slog.LevelError
	default:
		// Info, Progress, Success, and any future levels render at info —
		// the level enum is carried by slog.Level, the semantic distinction
		// stays as the Update.Level value (and can be re-derived from the
		// status.Level attr if a handler wants it).
		return slog.LevelInfo
	}
}
