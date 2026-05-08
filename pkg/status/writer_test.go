package status

import (
	"context"
	"testing"
)

// recordingCtx returns a context with an attached status channel sized to
// capture every emitted Update without dropping, plus a drain helper.
func recordingCtx(t *testing.T) (context.Context, func() []Update) {
	t.Helper()
	ch := make(chan Update, 1024)
	ctx := WithChannel(context.Background(), (chan<- Update)(ch))
	drain := func() []Update {
		var out []Update
		for {
			select {
			case u := <-ch:
				out = append(out, u)
			default:
				return out
			}
		}
	}
	return ctx, drain
}

// echoMapper is a minimal LineMapper used in tests: every line becomes an
// info-level Update with the line as Message.
func echoMapper(line []byte) Update {
	return NewUpdate(LevelInfo, string(line))
}

func TestWriter(t *testing.T) {
	t.Run("emits one update per line", func(t *testing.T) {
		ctx, drain := recordingCtx(t)
		w := NewWriter(ctx, echoMapper)
		_, _ = w.Write([]byte("line one\nline two\nline three\n"))

		got := drain()
		if len(got) != 3 {
			t.Fatalf("got %d updates, want 3", len(got))
		}
		for i, want := range []string{"line one", "line two", "line three"} {
			if got[i].Message != want {
				t.Errorf("update %d message = %q, want %q", i, got[i].Message, want)
			}
		}
	})

	t.Run("buffers partial line across writes", func(t *testing.T) {
		ctx, drain := recordingCtx(t)
		w := NewWriter(ctx, echoMapper)
		_, _ = w.Write([]byte("hello"))
		// Without a newline, no update should have fired.
		if mid := drain(); len(mid) != 0 {
			t.Fatalf("got %d updates after partial write, want 0", len(mid))
		}
		_, _ = w.Write([]byte(" world\n"))

		got := drain()
		if len(got) != 1 || got[0].Message != "hello world" {
			t.Fatalf("got %+v, want one update with message 'hello world'", got)
		}
	})

	t.Run("Flush emits trailing line without newline", func(t *testing.T) {
		ctx, drain := recordingCtx(t)
		w := NewWriter(ctx, echoMapper)
		_, _ = w.Write([]byte("trailing"))
		// Without Flush the line stays buffered.
		if mid := drain(); len(mid) != 0 {
			t.Fatalf("got %d updates before Flush, want 0", len(mid))
		}
		w.Flush()

		got := drain()
		if len(got) != 1 || got[0].Message != "trailing" {
			t.Fatalf("got %+v, want one update with message 'trailing'", got)
		}
	})

	t.Run("skips blank lines", func(t *testing.T) {
		ctx, drain := recordingCtx(t)
		w := NewWriter(ctx, echoMapper)
		_, _ = w.Write([]byte("\n  \nuseful\n\n"))

		got := drain()
		if len(got) != 1 || got[0].Message != "useful" {
			t.Fatalf("got %+v, want one update with message 'useful'", got)
		}
	})

	t.Run("strips trailing CR and LF", func(t *testing.T) {
		ctx, drain := recordingCtx(t)
		w := NewWriter(ctx, echoMapper)
		_, _ = w.Write([]byte("dos line\r\nunix line\n"))

		got := drain()
		if len(got) != 2 {
			t.Fatalf("got %d updates, want 2", len(got))
		}
		if got[0].Message != "dos line" {
			t.Errorf("dos line message = %q", got[0].Message)
		}
		if got[1].Message != "unix line" {
			t.Errorf("unix line message = %q", got[1].Message)
		}
	})
}

func TestRawMapper(t *testing.T) {
	t.Run("emits each line at the configured level", func(t *testing.T) {
		ctx, drain := recordingCtx(t)
		w := NewWriter(ctx, RawMapper(LevelError))
		_, _ = w.Write([]byte("connection refused\n"))

		got := drain()
		if len(got) != 1 {
			t.Fatalf("got %d updates, want 1", len(got))
		}
		if got[0].Level != LevelError {
			t.Errorf("level = %v, want %v", got[0].Level, LevelError)
		}
		if got[0].Message != "connection refused" {
			t.Errorf("message = %q", got[0].Message)
		}
	})
}

// TestConventionKeys pins the documented Metadata key names so a rename forces
// a deliberate update across all consumers.
func TestConventionKeys(t *testing.T) {
	if MetadataKeyPayload != "payload" {
		t.Errorf("MetadataKeyPayload = %q, want %q", MetadataKeyPayload, "payload")
	}
}
