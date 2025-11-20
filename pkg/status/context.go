package status

import "context"

// contextKey is an unexported type for context keys to avoid collisions
type contextKey string

// statusChannelKey is the context key for the status channel
const statusChannelKey contextKey = "status-channel"

// WithChannel returns a new context with the status channel attached
// The channel should be buffered to prevent blocking the sender
func WithChannel(ctx context.Context, ch chan<- StatusUpdate) context.Context {
	return context.WithValue(ctx, statusChannelKey, ch)
}

// getChannel retrieves the status channel from the context
// Returns nil if no channel is present in the context
func getChannel(ctx context.Context) chan<- StatusUpdate {
	if ctx == nil {
		return nil
	}

	ch, ok := ctx.Value(statusChannelKey).(chan<- StatusUpdate)
	if !ok {
		return nil
	}

	return ch
}

// HasChannel returns true if the context contains a status channel
func HasChannel(ctx context.Context) bool {
	return getChannel(ctx) != nil
}
