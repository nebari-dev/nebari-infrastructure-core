package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
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
func tofuVersionLine(ctx context.Context) string {
	resolved, err := tofu.ResolveExternal(ctx)
	switch {
	case err != nil:
		return fmt.Sprintf("unresolved (%v)", err)
	case resolved == nil:
		return fmt.Sprintf("%s (downloaded by nic)", tofu.Version)
	case resolved.Source == tofu.SourceOverride:
		return fmt.Sprintf("%s (from %s: %s)", resolved.Version, tofu.EnvTofuPath, resolved.Path)
	default:
		return fmt.Sprintf("%s (from PATH: %s)", resolved.Version, resolved.Path)
	}
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
