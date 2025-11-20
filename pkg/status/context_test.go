package status

import (
	"context"
	"testing"
)

func TestWithChannel(t *testing.T) {
	ch := make(chan StatusUpdate, 10)
	ctx := WithChannel(context.Background(), ch)

	if ctx == nil {
		t.Fatal("WithChannel returned nil context")
	}

	retrieved := getChannel(ctx)
	if retrieved == nil {
		t.Fatal("getChannel returned nil")
	}
}

func TestGetChannel_NoChannel(t *testing.T) {
	ctx := context.Background()
	ch := getChannel(ctx)

	if ch != nil {
		t.Error("getChannel should return nil when no channel in context")
	}
}

func TestGetChannel_NilContext(t *testing.T) {
	// Note: passing nil context to test edge case behavior
	// In production code, context.TODO() or context.Background() should be used
	ch := getChannel(nil) //nolint:staticcheck // Testing nil context handling

	if ch != nil {
		t.Error("getChannel should return nil for nil context")
	}
}

func TestGetChannel_WrongType(t *testing.T) {
	// Create context with wrong type value
	ctx := context.WithValue(context.Background(), statusChannelKey, "not a channel")
	ch := getChannel(ctx)

	if ch != nil {
		t.Error("getChannel should return nil when context value is wrong type")
	}
}

func TestHasChannel(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{
			name: "context with channel",
			ctx:  WithChannel(context.Background(), make(chan StatusUpdate, 10)),
			want: true,
		},
		{
			name: "context without channel",
			ctx:  context.Background(),
			want: false,
		},
		{
			name: "nil context",
			ctx:  nil,
			want: false,
		},
		{
			name: "context with wrong type",
			ctx:  context.WithValue(context.Background(), statusChannelKey, "not a channel"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasChannel(tt.ctx); got != tt.want {
				t.Errorf("HasChannel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextChaining(t *testing.T) {
	// Test that channel survives context chaining
	type testKey string
	otherKey := testKey("other-key")

	ch := make(chan StatusUpdate, 10)
	ctx := context.Background()
	ctx = WithChannel(ctx, ch)
	ctx = context.WithValue(ctx, otherKey, "other-value")

	if !HasChannel(ctx) {
		t.Error("Channel lost after adding another context value")
	}

	retrieved := getChannel(ctx)
	if retrieved == nil {
		t.Error("getChannel returned nil after context chaining")
	}
}

func TestMultipleChannels(t *testing.T) {
	// Test that replacing channel works correctly
	ch1 := make(chan StatusUpdate, 10)
	ch2 := make(chan StatusUpdate, 10)

	ctx := context.Background()
	ctx = WithChannel(ctx, ch1)
	ctx = WithChannel(ctx, ch2)

	// Send a message - should go to ch2
	Send(ctx, NewStatusUpdate(LevelInfo, "test"))

	select {
	case <-ch1:
		t.Error("Message sent to old channel")
	case <-ch2:
		// Success - message went to new channel
	default:
		t.Error("Message not sent to any channel")
	}
}
