package action

import (
	"log/slog"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// defaultStatusHandler returns a status.Handler that forwards progress updates
// to slog.Default() with the level and structured attributes preserved. It is
// the built-in sink used by every action so progress flows through the caller's
// existing slog configuration without any extra wiring.
func defaultStatusHandler() status.Handler {
	return func(update status.Update) {
		attrs := []any{"message", update.Message}

		if update.Resource != "" {
			attrs = append(attrs, "resource", update.Resource)
		}
		if update.Action != "" {
			attrs = append(attrs, "action", update.Action)
		}
		for key, value := range update.Metadata {
			attrs = append(attrs, key, value)
		}

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
