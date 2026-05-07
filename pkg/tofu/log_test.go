package tofu

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// recordingHandler captures slog.Records into a slice for inspection.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func recordingLogger() (*slog.Logger, *recordingHandler) {
	h := &recordingHandler{}
	return slog.New(h), h
}

// attrMap collects all attrs of a slog.Record into a map keyed by attr name.
func attrMap(r slog.Record) map[string]slog.Value {
	m := make(map[string]slog.Value)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

func TestEmitJSONStream(t *testing.T) {
	t.Run("maps each level correctly", func(t *testing.T) {
		input := strings.Join([]string{
			`{"@level":"trace","@message":"trace msg","type":"log"}`,
			`{"@level":"debug","@message":"debug msg","type":"log"}`,
			`{"@level":"info","@message":"info msg","type":"log"}`,
			`{"@level":"warn","@message":"warn msg","type":"log"}`,
			`{"@level":"error","@message":"error msg","type":"log"}`,
		}, "\n") + "\n"

		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(input), logger)

		want := []struct {
			level slog.Level
			msg   string
		}{
			{slog.LevelDebug, "trace msg"},
			{slog.LevelDebug, "debug msg"},
			{slog.LevelInfo, "info msg"},
			{slog.LevelWarn, "warn msg"},
			{slog.LevelError, "error msg"},
		}
		if len(h.records) != len(want) {
			t.Fatalf("got %d records, want %d", len(h.records), len(want))
		}
		for i, w := range want {
			if h.records[i].Level != w.level {
				t.Errorf("record %d: level = %v, want %v", i, h.records[i].Level, w.level)
			}
			if h.records[i].Message != w.msg {
				t.Errorf("record %d: msg = %q, want %q", i, h.records[i].Message, w.msg)
			}
		}
	})

	t.Run("preserves typed payload fields", func(t *testing.T) {
		// Drives one event per enumerated payload field through the parser and
		// confirms each surfaces as a slog attr carrying the original JSON
		// bytes (so a slog JSON handler renders nested objects inline).
		cases := []struct {
			name   string
			line   string
			attr   string
			needle string
		}{
			{
				name:   "hook on apply_start",
				line:   `{"@level":"info","@message":"creating","type":"apply_start","hook":{"resource":{"addr":"aws_eks_cluster.this"},"action":"create"}}`,
				attr:   "hook",
				needle: "aws_eks_cluster.this",
			},
			{
				name:   "change on planned_change",
				line:   `{"@level":"info","@message":"plan","type":"planned_change","change":{"resource":{"addr":"aws_vpc.main"},"action":"create"}}`,
				attr:   "change",
				needle: "aws_vpc.main",
			},
			{
				name:   "changes on change_summary",
				line:   `{"@level":"info","@message":"Plan: 1 to add","type":"change_summary","changes":{"add":1,"change":0,"remove":0,"operation":"plan"}}`,
				attr:   "changes",
				needle: `"add":1`,
			},
			{
				name:   "outputs on outputs event",
				line:   `{"@level":"info","@message":"Outputs: 1","type":"outputs","outputs":{"animal_name":{"value":"clever-shark","type":"string"}}}`,
				attr:   "outputs",
				needle: "clever-shark",
			},
			{
				name:   "diagnostic on error",
				line:   `{"@level":"error","@message":"Error: bad","type":"diagnostic","diagnostic":{"severity":"error","summary":"bad"}}`,
				attr:   "diagnostic",
				needle: `"severity":"error"`,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				logger, h := recordingLogger()
				emitJSONStream(context.Background(), strings.NewReader(tc.line+"\n"), logger)

				if len(h.records) != 1 {
					t.Fatalf("got %d records, want 1", len(h.records))
				}
				attrs := attrMap(h.records[0])

				if got := attrs["source"].String(); got != logSource {
					t.Errorf("source = %q, want %q", got, logSource)
				}
				payload, ok := attrs[tc.attr]
				if !ok {
					t.Fatalf("%s attr missing", tc.attr)
				}
				raw, ok := payload.Any().(json.RawMessage)
				if !ok {
					t.Fatalf("%s attr type = %T, want json.RawMessage", tc.attr, payload.Any())
				}
				if !strings.Contains(string(raw), tc.needle) {
					t.Errorf("%s payload = %s, want substring %q", tc.attr, raw, tc.needle)
				}
			})
		}
	})

	t.Run("drops noise fields", func(t *testing.T) {
		// @module is always "tofu.ui" for UI events; @timestamp is redundant
		// with slog's own record time; tofu/ui appear only on the version event
		// and aren't useful. None should reach the slog record as attrs.
		line := `{"@level":"info","@message":"OpenTofu 1.11.3","@module":"tofu.ui","@timestamp":"2026-05-06T14:00:00Z","type":"version","tofu":"1.11.3","ui":"1.2"}`
		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(line+"\n"), logger)

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
		attrs := attrMap(h.records[0])
		for _, k := range []string{"@module", "module", "@timestamp", "tofu", "ui"} {
			if _, ok := attrs[k]; ok {
				t.Errorf("attr %q should be absent (noise field)", k)
			}
		}
		if _, ok := attrs["type"]; !ok {
			t.Error("type attr should still be present")
		}
	})

	t.Run("omits payload attrs the event did not emit", func(t *testing.T) {
		line := `{"@level":"info","@message":"hello","type":"log"}`
		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(line+"\n"), logger)

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
		attrs := attrMap(h.records[0])
		for _, k := range []string{"hook", "change", "changes", "outputs", "diagnostic"} {
			if _, ok := attrs[k]; ok {
				t.Errorf("attr %q should be absent", k)
			}
		}
	})

	t.Run("malformed line falls through as info-level raw text", func(t *testing.T) {
		input := "this is not JSON\n" +
			`{"@level":"info","@message":"valid","type":"log"}` + "\n"

		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(input), logger)

		if len(h.records) != 2 {
			t.Fatalf("got %d records, want 2", len(h.records))
		}
		if h.records[0].Message != "this is not JSON" {
			t.Errorf("fallthrough message = %q", h.records[0].Message)
		}
		if h.records[0].Level != slog.LevelInfo {
			t.Errorf("fallthrough level = %v, want info", h.records[0].Level)
		}
		if h.records[1].Message != "valid" {
			t.Errorf("valid message = %q", h.records[1].Message)
		}
	})

	t.Run("skips blank lines", func(t *testing.T) {
		input := "\n   \n" + `{"@level":"info","@message":"hello","type":"log"}` + "\n\n"
		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(input), logger)

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
	})

	t.Run("handles lines exceeding default scanner buffer", func(t *testing.T) {
		// 100KB > bufio.Scanner's default 64KB cap; planned-change events can
		// approach this in real workloads.
		bigMsg := strings.Repeat("x", 100*1024)
		line := `{"@level":"info","@message":"` + bigMsg + `","type":"log"}` + "\n"

		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(line), logger)

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
		if h.records[0].Message != bigMsg {
			t.Errorf("big message corrupted (len %d, want %d)", len(h.records[0].Message), len(bigMsg))
		}
	})

	t.Run("unknown level defaults to info", func(t *testing.T) {
		line := `{"@level":"weird","@message":"unknown","type":"log"}` + "\n"
		logger, h := recordingLogger()
		emitJSONStream(context.Background(), strings.NewReader(line), logger)

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
		if h.records[0].Level != slog.LevelInfo {
			t.Errorf("level = %v, want info", h.records[0].Level)
		}
	})

	t.Run("returns when reader hits EOF", func(t *testing.T) {
		// io.Pipe blocks the reader until the writer is closed. emitJSONStream
		// must observe EOF so wrapper methods can drain by closing the writer.
		pr, pw := io.Pipe()
		logger, h := recordingLogger()

		done := make(chan struct{})
		go func() {
			emitJSONStream(context.Background(), pr, logger)
			close(done)
		}()

		_, err := pw.Write([]byte(`{"@level":"info","@message":"first","type":"log"}` + "\n"))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}

		if err := pw.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		<-done

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
	})
}

func TestEmitRawStream(t *testing.T) {
	t.Run("emits each line at requested level with source attr", func(t *testing.T) {
		input := "line one\nline two\n"
		logger, h := recordingLogger()
		emitRawStream(context.Background(), strings.NewReader(input), logger, slog.LevelError)

		if len(h.records) != 2 {
			t.Fatalf("got %d records, want 2", len(h.records))
		}
		for i, want := range []string{"line one", "line two"} {
			if h.records[i].Message != want {
				t.Errorf("record %d msg = %q, want %q", i, h.records[i].Message, want)
			}
			if h.records[i].Level != slog.LevelError {
				t.Errorf("record %d level = %v, want error", i, h.records[i].Level)
			}
			if got := attrMap(h.records[i])["source"].String(); got != logSource {
				t.Errorf("record %d source = %q, want %q", i, got, logSource)
			}
		}
	})

	t.Run("skips blank lines", func(t *testing.T) {
		input := "\n  \nuseful\n\n"
		logger, h := recordingLogger()
		emitRawStream(context.Background(), strings.NewReader(input), logger, slog.LevelInfo)

		if len(h.records) != 1 {
			t.Fatalf("got %d records, want 1", len(h.records))
		}
		if h.records[0].Message != "useful" {
			t.Errorf("msg = %q, want %q", h.records[0].Message, "useful")
		}
	})
}
