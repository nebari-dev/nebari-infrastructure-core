package renderer

import (
	"bytes"
	"sync"
)

// lineWriter is an io.Writer that buffers input and calls r.Detail() for each
// complete line. It is goroutine-safe.
type lineWriter struct {
	r   Renderer
	mu  sync.Mutex
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if line != "" {
			w.r.Detail(line)
		}
	}
	return len(p), nil
}

// Flush sends any remaining buffered content as a detail line.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		w.r.Detail(string(w.buf))
		w.buf = w.buf[:0]
	}
}
