package tofu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

func TestMapStatusLevel(t *testing.T) {
	// Per OpenTofu's machine-readable UI spec, @level is "info" by default
	// and may be "warn" or "error" when surfacing diagnostics. Anything else
	// (including an empty string) falls through to info.
	cases := []struct {
		in   string
		want status.Level
	}{
		{"info", status.LevelInfo},
		{"warn", status.LevelWarning},
		{"error", status.LevelError},
		{"warning", status.LevelInfo}, // not in spec
		{"weird", status.LevelInfo},
		{"", status.LevelInfo},
	}
	for _, tc := range cases {
		name := tc.in
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got := mapStatusLevel(tc.in); got != tc.want {
				t.Errorf("mapStatusLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestJSONLineMapper(t *testing.T) {
	t.Run("extracts level and message into Update fields", func(t *testing.T) {
		line := []byte(`{"@level":"warn","@message":"deprecated argument","type":"diagnostic"}`)
		got := jsonLineMapper(line)
		if got.Level != status.LevelWarning {
			t.Errorf("level = %v, want %v", got.Level, status.LevelWarning)
		}
		if got.Message != "deprecated argument" {
			t.Errorf("message = %q", got.Message)
		}
	})

	t.Run("forwards the full event as detail metadata", func(t *testing.T) {
		// Drives one event per kind of detail through the mapper and
		// confirms the entire JSON rides through as MetadataKeyDetail.
		// We don't enumerate fields — consumers decode whatever they need.
		cases := []struct {
			name    string
			line    string
			needles []string
		}{
			{
				name:    "hook on apply_start",
				line:    `{"@level":"info","@message":"creating","type":"apply_start","hook":{"resource":{"addr":"aws_eks_cluster.this"},"action":"create"}}`,
				needles: []string{"apply_start", "aws_eks_cluster.this"},
			},
			{
				name:    "change on planned_change",
				line:    `{"@level":"info","@message":"plan","type":"planned_change","change":{"resource":{"addr":"aws_vpc.main"},"action":"create"}}`,
				needles: []string{"planned_change", "aws_vpc.main"},
			},
			{
				name:    "diagnostic on error",
				line:    `{"@level":"error","@message":"Error: bad","type":"diagnostic","diagnostic":{"severity":"error","summary":"bad"}}`,
				needles: []string{"diagnostic", `"severity":"error"`},
			},
			{
				name:    "version event with otherwise-noise fields",
				line:    `{"@level":"info","@message":"OpenTofu 1.11.3","@module":"tofu.ui","tofu":"1.11.3","ui":"1.2","type":"version"}`,
				needles: []string{"OpenTofu 1.11.3", `"tofu":"1.11.3"`, `"@module":"tofu.ui"`},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := jsonLineMapper([]byte(tc.line))
				raw, ok := got.Metadata[status.MetadataKeyDetail].(json.RawMessage)
				if !ok {
					t.Fatalf("detail metadata type = %T, want json.RawMessage",
						got.Metadata[status.MetadataKeyDetail])
				}
				for _, needle := range tc.needles {
					if !strings.Contains(string(raw), needle) {
						t.Errorf("detail = %s, want substring %q", raw, needle)
					}
				}
			})
		}
	})

	t.Run("malformed JSON falls through as raw text at info level", func(t *testing.T) {
		got := jsonLineMapper([]byte("this is not JSON"))
		if got.Level != status.LevelInfo {
			t.Errorf("level = %v, want info", got.Level)
		}
		if got.Message != "this is not JSON" {
			t.Errorf("message = %q", got.Message)
		}
		if _, ok := got.Metadata[status.MetadataKeyDetail]; ok {
			t.Errorf("detail metadata should be absent for non-JSON input")
		}
	})

	t.Run("unknown @level falls through to info", func(t *testing.T) {
		// Forward-compatibility: if tofu introduces a new @level we don't
		// recognise, the event still flows through (info level) with the
		// full detail available for any consumer that does care.
		got := jsonLineMapper([]byte(`{"@level":"fancy","@message":"x","type":"log"}`))
		if got.Level != status.LevelInfo {
			t.Errorf("level = %v, want info", got.Level)
		}
		if got.Message != "x" {
			t.Errorf("message = %q, want %q", got.Message, "x")
		}
	})
}
