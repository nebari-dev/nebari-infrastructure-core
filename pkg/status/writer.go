package status

import (
	"bytes"
	"context"
	"io"
)

// MetadataKeySource is the conventional Metadata key on Updates whose value
// identifies the producing component (e.g. "tofu", "aws", "argocd"). Consumers
// dispatching by source — pretty-printers, UI bridges, filters — read this key.
const MetadataKeySource = "source"

// MetadataKeyPayload is the conventional Metadata key on Updates carrying a
// structured per-source payload (typically a json.RawMessage). The shape of
// the payload depends on the source; consumers read MetadataKeySource first
// to know how to decode it.
const MetadataKeyPayload = "payload"

// LineMapper converts one line of subprocess output into a status.Update.
// The line has trailing newline / carriage return stripped. Mappers should
// return a populated Update (the Writer skips blank lines on its own, so the
// mapper is always invoked with non-empty input).
type LineMapper func(line []byte) Update

// Writer is an io.Writer that splits the bytes written to it on newlines and
// emits one status.Update per non-blank line via the configured LineMapper.
//
// Partial lines are buffered across Write calls until a newline arrives; call
// Flush after the producer finishes writing to drain a final line that wasn't
// newline-terminated.
//
// Writer is not safe for concurrent use. tfexec and os/exec each drive a
// single io.Copy goroutine per stream, so single-writer is the only shape we
// see in practice.
type Writer struct {
	ctx    context.Context
	mapper LineMapper
	buf    bytes.Buffer
}

// NewWriter returns a Writer that emits via mapper to the status channel
// attached to ctx. mapper must not be nil.
func NewWriter(ctx context.Context, mapper LineMapper) *Writer {
	return &Writer{ctx: ctx, mapper: mapper}
}

// Write implements io.Writer. It returns len(p), nil — Writes never fail
// because the underlying status.Send is non-blocking and the line-buffering
// has nowhere to error.
func (w *Writer) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadBytes('\n')
		if err != nil {
			// Incomplete line; put remainder back for the next Write.
			w.buf.Reset()
			w.buf.Write(line)
			break
		}
		w.emit(bytes.TrimRight(line, "\r\n"))
	}
	return len(p), nil
}

// Flush emits any buffered partial line as a final Update. Call after the
// producer has finished writing to handle a stream that exits without a
// trailing newline.
func (w *Writer) Flush() {
	if w.buf.Len() > 0 {
		w.emit(bytes.TrimRight(w.buf.Bytes(), "\r\n"))
		w.buf.Reset()
	}
}

func (w *Writer) emit(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	Send(w.ctx, w.mapper(line))
}

// RawMapper returns a LineMapper that emits each line verbatim as the
// Update's Message at the given level. Use it for subprocesses that emit
// plain-text output (no structured format). Source attribution, when added,
// will flow through ctx rather than being passed in here.
func RawMapper(level Level) LineMapper {
	return func(line []byte) Update {
		return NewUpdate(level, string(line))
	}
}

// Compile-time check.
var _ io.Writer = (*Writer)(nil)
