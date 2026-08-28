package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

// These are set at build time via -ldflags, e.g.
// "-X github.com/nebari-dev/nebari-infrastructure-core/internal/cli.version=...".
// The -X target is this package's import path, not "main": these vars live in
// package cli, and the linker matches -X against the fully-qualified package
// path, so pointing it at main.version (as when this lived in cmd/nic) silently
// injects nothing. See Makefile and .goreleaser.yml, which set all three.
//
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
// returned so `nic version` stays usable as a diagnostic. Resolution notes
// (e.g. why a tofu found on PATH was rejected) are folded into the line so the
// command explains the packaging problem it exists to diagnose.
func tofuVersionLine(ctx context.Context) string {
	resolution, err := tofu.ResolveExternal(ctx)
	switch {
	case err != nil:
		return fmt.Sprintf("unresolved (%v)", err)
	case resolution.Binary == nil:
		if len(resolution.Notes) > 0 {
			return fmt.Sprintf("%s (downloaded by nic; %s)", tofu.Version, strings.Join(resolution.Notes, "; "))
		}
		return fmt.Sprintf("%s (downloaded by nic)", tofu.Version)
	case resolution.Binary.Source == tofu.SourceOverride:
		return fmt.Sprintf("%s (from %s: %s%s)", resolution.Binary.Version, tofu.EnvTofuPath, resolution.Binary.Path, testedAgainstNote(resolution.Binary.Version))
	default:
		return fmt.Sprintf("%s (from PATH: %s%s)", resolution.Binary.Version, resolution.Binary.Path, testedAgainstNote(resolution.Binary.Version))
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
	fmt.Printf("Registered repository providers: %v\n", providers.Repository)

	return nil
}
