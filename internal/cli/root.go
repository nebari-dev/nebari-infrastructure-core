package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// reachedRunE reports whether cobra parsed flags and validated args
// successfully, i.e. PersistentPreRun ran and a command's RunE is about to
// (or did) execute. Execute uses it to distinguish runtime failures (which
// should be logged) from usage-class errors (bad flag, unknown command,
// wrong number of args), which surface before PersistentPreRun and are
// already printed by cobra.
var reachedRunE bool

// NewRootCmd builds a fresh nic command tree. Building a tree with this
// function and never calling Execute/ExecuteC on it (as cmd/docgen does, to
// walk the tree for documentation) means cobra never runs
// InitDefaultCompletionCmd - the completion subcommand is never attached in
// the first place.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nic",
		Short: "Nebari Infrastructure Core - Cloud infrastructure management for Nebari",
		Long: `Nebari Infrastructure Core (NIC) is a standalone CLI tool that manages
cloud infrastructure for Nebari using native cloud SDKs with declarative semantics.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			slog.SetDefault(logger)

			// PersistentPreRun runs only after cobra has parsed flags and validated
			// args. Any failure from here on is a runtime error and not a misuse,
			// so silence cobra's own error/usage output and let Execute's caller
			// report it once via slog. Usage-class errors (bad flag, unknown
			// command, wrong number of args) surface before this hook runs, so
			// cobra still prints the error and usage block for those.
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			reachedRunE = true
		},
	}

	root.AddCommand(deployCmd)
	root.AddCommand(destroyCmd)
	root.AddCommand(validateCmd)
	root.AddCommand(versionCmd)
	root.AddCommand(kubeconfigCmd)

	return root
}

// RunError wraps an error that occurred after PersistentPreRun ran - a
// genuine runtime failure, as opposed to a cobra usage error (bad flag,
// unknown command, wrong number of args) that cobra already printed itself.
type RunError struct{ Err error }

func (e *RunError) Error() string { return e.Err.Error() }
func (e *RunError) Unwrap() error { return e.Err }

// Execute builds a fresh command tree and runs it against ctx.
func Execute(ctx context.Context) error {
	err := NewRootCmd().ExecuteContext(ctx)
	if err != nil && reachedRunE {
		return &RunError{Err: err}
	}
	return err
}
