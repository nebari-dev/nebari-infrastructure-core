package nic

import (
	"context"
	"log/slog"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// SlogHandler renders status Updates as slog records on the supplied logger.
// It returns the handler function itself, so callers can wrap it (e.g., to
// filter, add fields, or fan out to a second sink) before passing the result
// to status.StartHandler. For plain slog-only integration, use StartSlogHandler.
func SlogHandler(logger *slog.Logger) status.Handler {
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

// StartSlogHandler wires Client progress to the supplied slog logger. Pass
// the returned ctx to subsequent Client calls, and defer the returned
// cleanup to flush in-flight updates. Consumers who want a non-slog sink
// should call status.StartHandler with their own handler instead.
func StartSlogHandler(ctx context.Context, logger *slog.Logger) (context.Context, status.CleanupFunc) {
	return status.StartHandler(ctx, SlogHandler(logger))
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
