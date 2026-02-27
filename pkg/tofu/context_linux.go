package tofu

import "context"

// signalSafeContext returns a SIGINT safe context for tofu operations. On Linux,
// terraform-exec sets Setpgid: true, placing tofu in its own process group isolated
// from the terminal's foreground group. Tofu receives exactly one SIGINT on Ctrl+C,
// so the context is passed through unchanged.
func signalSafeContext(ctx context.Context) context.Context {
	return ctx
}
