//go:build !linux

package tofu

import "context"

// signalSafeContext returns a SIGINT safe context for tofu operations. On non-Linux
// platforms, terraform-exec does not set Setpgid: true, so tofu inherits the parent's
// process group and receives Ctrl+C directly from the terminal. This results in tofu
// receiving two SIGINTs instead of one, causing it to abort immediately. Detaching
// from the parent context ensures tofu receives only one.
func signalSafeContext(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}
