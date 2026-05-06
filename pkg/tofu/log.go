package tofu

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
)

// logSource is the value of the "source" attr on every slog record emitted
// from OpenTofu output. It lets a slog handler discriminate tofu output from
// the application's own logs.
const logSource = "tofu"

func mapLevel(s string) slog.Level {
	switch s {
	case "trace", "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// tofuEvent is one machine-readable UI event emitted by `tofu <command> -json`.
// See https://opentofu.org/docs/internals/machine-readable-ui/.
//
// The structured payloads (Hook/Change/Changes/Outputs/Diag) are kept as
// json.RawMessage so the slog JSON handler emits them inline as nested
// objects (json.RawMessage implements json.Marshaler) without us hard-coding
// every sub-schema. Consumers decode the fields they need. Fields not listed
// here (e.g. @module, @timestamp, version-event extras) are intentionally
// dropped as noise.
type tofuEvent struct {
	Level   string          `json:"@level"`
	Message string          `json:"@message"`
	Type    string          `json:"type"`
	Hook    json.RawMessage `json:"hook,omitempty"`
	Change  json.RawMessage `json:"change,omitempty"`
	Changes json.RawMessage `json:"changes,omitempty"`
	Outputs json.RawMessage `json:"outputs,omitempty"`
	Diag    json.RawMessage `json:"diagnostic,omitempty"`
}

// emitJSONStream reads JSON-line events from r and emits one slog record per
// line on logger. It returns when r hits EOF, so the caller controls
// termination by closing the writer end of the pipe.
//
// Lines that don't parse as JSON fall through as plain info-level records so
// unexpected stdout text never disappears silently.
func emitJSONStream(ctx context.Context, r io.Reader, logger *slog.Logger) {
	scanner := bufio.NewScanner(r)
	// Tofu planned-change events can exceed the default 64KB scanner buffer.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev tofuEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			logger.LogAttrs(ctx, slog.LevelInfo, string(line),
				slog.String("source", logSource),
			)
			continue
		}
		attrs := []slog.Attr{slog.String("source", logSource)}
		if ev.Type != "" {
			attrs = append(attrs, slog.String("type", ev.Type))
		}
		if len(ev.Hook) > 0 {
			attrs = append(attrs, slog.Any("hook", ev.Hook))
		}
		if len(ev.Change) > 0 {
			attrs = append(attrs, slog.Any("change", ev.Change))
		}
		if len(ev.Changes) > 0 {
			attrs = append(attrs, slog.Any("changes", ev.Changes))
		}
		if len(ev.Outputs) > 0 {
			attrs = append(attrs, slog.Any("outputs", ev.Outputs))
		}
		if len(ev.Diag) > 0 {
			attrs = append(attrs, slog.Any("diagnostic", ev.Diag))
		}
		logger.LogAttrs(ctx, mapLevel(ev.Level), ev.Message, attrs...)
	}
}

// emitRawStream reads r line-by-line and emits one slog record per line on
// logger at the given level. Used for streams that don't emit JSON (stderr,
// or stdout from operations that lack a -json variant).
func emitRawStream(ctx context.Context, r io.Reader, logger *slog.Logger, level slog.Level) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		logger.LogAttrs(ctx, level, string(line), slog.String("source", logSource))
	}
}
