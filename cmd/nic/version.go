package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

// These are set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
// They MUST be var (not const): the Go linker's -X flag can only override package-level
// string variables, so declaring them const silently discards the injected values and the
// binary reports these defaults regardless of how it was built.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the version information for Nebari Infrastructure Core (NIC).`,
	RunE:  runVersion,
}

// tofuVersionLine describes which OpenTofu binary NIC would use: an external
// one resolved via NIC_TOFU_PATH or PATH, or the pinned version it downloads.
// Resolution errors (e.g. a bad NIC_TOFU_PATH) are reported rather than
// returned so `nic version` stays usable as a diagnostic.
//
// Resolution emits its diagnostics (e.g. "ignoring tofu on PATH: too old") as
// status updates, which are dropped unless a status channel is attached to ctx.
// A handler is attached here to capture warnings and fold them into the line;
// without it the command could not explain why an external binary was rejected,
// which is the packaging problem this line exists to diagnose.
func tofuVersionLine(ctx context.Context) string {
	var warnings []string
	ctx, cleanup := status.StartHandler(ctx, func(update status.Update) {
		if update.Level == status.LevelWarning {
			warnings = append(warnings, update.Message)
		}
	})
	resolved, err := tofu.ResolveExternal(ctx)
	// cleanup closes the channel and waits for the handler goroutine, so
	// warnings is safe to read afterwards.
	cleanup()

	switch {
	case err != nil:
		return fmt.Sprintf("unresolved (%v)", err)
	case resolved == nil:
		if len(warnings) > 0 {
			return fmt.Sprintf("%s (downloaded by nic; %s)", tofu.Version, strings.Join(warnings, "; "))
		}
		return fmt.Sprintf("%s (downloaded by nic)", tofu.Version)
	case resolved.Source == tofu.SourceOverride:
		return fmt.Sprintf("%s (from %s: %s%s)", resolved.Version, tofu.EnvTofuPath, resolved.Path, testedAgainstNote(resolved.Version))
	default:
		return fmt.Sprintf("%s (from PATH: %s%s)", resolved.Version, resolved.Path, testedAgainstNote(resolved.Version))
	}
}

// testedAgainstNote flags an external version that differs from the pinned one
// NIC is tested against, so support requests carry the mismatch.
func testedAgainstNote(ver string) string {
	if ver == tofu.Version {
		return ""
	}
	return fmt.Sprintf("; NIC is tested against %s", tofu.Version)
}

func runVersion(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.version")
	defer span.End()

	slog.Info("Version command executed", "version", version, "commit", commit, "date", date)

	fmt.Printf("Nebari Infrastructure Core (NIC)\n")
	fmt.Printf("Version: %s\n", version)
	fmt.Printf("Commit: %s\n", commit)
	fmt.Printf("Built: %s\n", date)
	fmt.Printf("OpenTofu version: %s\n", tofuVersionLine(ctx))

	client, err := nic.NewClient(ctx)
	if err != nil {
		return err
	}
	providers := client.ProviderNames(ctx)
	fmt.Printf("Registered cloud providers: %v\n", providers.Cluster)
	fmt.Printf("Registered DNS providers: %v\n", providers.DNS)

	return nil
}
