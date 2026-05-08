package tofu

import (
	"encoding/json"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// tofuEvent captures the two fields we need to populate a status.Update's
// Level and Message. The full event JSON rides through as the Update's
// payload (status.MetadataKeyPayload) so consumers can decode any sub-field
// (hook/change/changes/outputs/diagnostic/etc.) without us enumerating them.
//
// See https://opentofu.org/docs/internals/machine-readable-ui/.
type tofuEvent struct {
	Level   string `json:"@level"`
	Message string `json:"@message"`
}

// mapStatusLevel converts a tofu UI event's @level field into the equivalent
// status.Level. Per the OpenTofu machine-readable UI spec, @level is one of
// "info" (normal), "warn", or "error" (surfacing diagnostics). Any other
// value, including an empty string, falls through to info.
func mapStatusLevel(s string) status.Level {
	switch s {
	case "info":
		return status.LevelInfo
	case "warn":
		return status.LevelWarning
	case "error":
		return status.LevelError
	default:
		return status.LevelInfo
	}
}

// jsonLineMapper is the status.LineMapper used for tofu's `-json` stdout. It
// parses each event line, extracts @level and @message into the Update's
// typed fields, and forwards the full event as the payload. Lines that
// don't parse as JSON fall through as raw text at info level so unexpected
// output is never lost.
func jsonLineMapper(line []byte) status.Update {
	var ev tofuEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return status.NewUpdate(status.LevelInfo, string(line))
	}
	// Copy the line so the payload isn't aliasing the Writer's internal
	// buffer (which gets reused on the next Write).
	payload := make(json.RawMessage, len(line))
	copy(payload, line)
	return status.NewUpdate(mapStatusLevel(ev.Level), ev.Message).
		WithMetadata(status.MetadataKeyPayload, payload)
}
