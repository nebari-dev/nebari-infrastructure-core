package main

import (
	"log/slog"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// statusLogHandler returns a status.Handler that logs updates using slog
// This keeps the logging concern in the application layer while reusing
// the status package's channel management
func statusLogHandler() status.Handler {
	return func(update status.Update) {
		// Build structured logging attributes
		attrs := []any{
			"message", update.Message,
		}

		if update.Resource != "" {
			attrs = append(attrs, "resource", update.Resource)
		}

		if update.Action != "" {
			attrs = append(attrs, "action", update.Action)
		}

		// Add metadata as individual attributes
		for key, value := range update.Metadata {
			attrs = append(attrs, key, value)
		}

		// Log at appropriate level
		switch update.Level {
		case status.LevelInfo:
			slog.Info("Status", attrs...)
		case status.LevelProgress:
			slog.Info("Progress", attrs...)
		case status.LevelSuccess:
			slog.Info("Success", attrs...)
		case status.LevelWarning:
			slog.Warn("Warning", attrs...)
		case status.LevelError:
			slog.Error("Error", attrs...)
		default:
			slog.Info("Status", attrs...)
		}
	}
}
