package renderer

import "context"

type ctxKey struct{}

// WithRenderer returns a new context carrying the given Renderer.
func WithRenderer(ctx context.Context, r Renderer) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

// FromContext retrieves the Renderer from the context.
// Returns nil if no renderer is present.
func FromContext(ctx context.Context) Renderer {
	r, _ := ctx.Value(ctxKey{}).(Renderer)
	return r
}
