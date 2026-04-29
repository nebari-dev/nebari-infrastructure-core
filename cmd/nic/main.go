package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider/cloudflare"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/aws"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/azure"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/existing"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/gcp"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/hetzner"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/local"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/telemetry"
)

var (
	// Global provider registry
	reg *registry.Registry

	// Global flags
	formatFlag  string
	verboseFlag bool

	// Root command
	rootCmd = &cobra.Command{
		Use:   "nic",
		Short: "Nebari Infrastructure Core - Cloud infrastructure management for Nebari",
		Long: `Nebari Infrastructure Core (NIC) is a standalone CLI tool that manages
cloud infrastructure for Nebari using native cloud SDKs with declarative semantics.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			r, err := selectRenderer(formatFlag, verboseFlag)
			if err != nil {
				return err
			}
			cmd.SetContext(renderer.WithRenderer(cmd.Context(), r))

			// For JSON mode, use the JSON renderer's slog logger as default.
			// For pretty/plain mode, discard slog output to avoid JSON noise.
			if jr, ok := r.(*renderer.JSON); ok {
				slog.SetDefault(jr.Logger())
			} else {
				slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			// Ensure the TUI is properly shut down when commands finish
			// without calling Summary() (e.g., version, validate).
			r := renderer.FromContext(cmd.Context())
			if t, ok := r.(*renderer.TUI); ok {
				t.Quit()
			}
			return nil
		},
	}
)

// selectRenderer chooses the Renderer based on the --format flag and TTY detection.
func selectRenderer(format string, verbose bool) (renderer.Renderer, error) {
	switch format {
	case "json":
		return renderer.NewJSON(os.Stderr), nil
	case "tui":
		return renderer.NewTUI(), nil
	case "pretty":
		return renderer.NewPretty(os.Stdout, verbose), nil
	case "plain":
		return renderer.NewPlain(os.Stdout, verbose), nil
	case "auto", "":
		if isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()) {
			if os.Getenv("NO_COLOR") != "" {
				return renderer.NewPlain(os.Stdout, verbose), nil
			}
			return renderer.NewPretty(os.Stdout, verbose), nil
		}
		return renderer.NewJSON(os.Stderr), nil
	default:
		return nil, fmt.Errorf("unknown format %q: must be auto, pretty, tui, json, or plain", format)
	}
}

func init() {
	// Register global persistent flags
	rootCmd.PersistentFlags().StringVar(&formatFlag, "format", "auto", "Output format: auto, tui, pretty, json, or plain")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Show detailed output from third-party tools")

	// Load .env file if it exists (silently ignore if not found)
	// This allows users to optionally use .env for local development
	_ = godotenv.Load()

	// Initialize provider registry
	reg = registry.NewRegistry()

	// Register all providers explicitly (no blank imports or init() magic)
	ctx := context.Background()

	if err := reg.ClusterProviders.Register(ctx, "aws", aws.NewProvider()); err != nil {
		log.Fatalf("Failed to register AWS provider: %v", err)
	}

	if err := reg.ClusterProviders.Register(ctx, "gcp", gcp.NewProvider()); err != nil {
		log.Fatalf("Failed to register GCP provider: %v", err)
	}

	if err := reg.ClusterProviders.Register(ctx, "azure", azure.NewProvider()); err != nil {
		log.Fatalf("Failed to register Azure provider: %v", err)
	}

	if err := reg.ClusterProviders.Register(ctx, "local", local.NewProvider()); err != nil {
		log.Fatalf("Failed to register local provider: %v", err)
	}

	if err := reg.ClusterProviders.Register(ctx, "hetzner", hetzner.NewProvider()); err != nil {
		log.Fatalf("Failed to register Hetzner provider: %v", err)
	}

	if err := reg.ClusterProviders.Register(ctx, "existing", existing.NewProvider()); err != nil {
		log.Fatalf("Failed to register existing provider: %v", err)
	}

	// Register DNS providers explicitly
	if err := reg.DNSProviders.Register(ctx, "cloudflare", cloudflare.NewProvider()); err != nil {
		log.Fatalf("Failed to register Cloudflare DNS provider: %v", err)
	}

	// Add subcommands
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(kubeconfigCmd)
}

func main() {
	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Launch goroutine to handle signals
	go func() {
		sig := <-sigChan
		slog.Warn("Received interrupt signal, initiating graceful shutdown", "signal", sig.String())
		cancel() // Cancel context to stop all operations
	}()

	// Setup OpenTelemetry
	_, shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		slog.Error("Failed to setup telemetry", "error", err)
		os.Exit(1) //nolint:gocritic // TODO: refactor to run() pattern to allow defers to run
	}
	defer func() {
		// Use a fresh context for shutdown in case main context is cancelled
		shutdownCtx := context.Background()
		if err := shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown telemetry", "error", err)
		}
	}()

	// Execute root command with cancellable context
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// Don't log error if it was due to context cancellation (graceful shutdown)
		if ctx.Err() == context.Canceled {
			slog.Info("Shutdown complete")
			os.Exit(130) // Exit code 130 indicates terminated by Ctrl+C
		}
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}
