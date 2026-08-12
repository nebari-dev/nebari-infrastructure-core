package main

import (
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

// versionString renders the version report. It is a pure helper (no client
// construction or I/O) so it can be unit-tested independently of runVersion.
func versionString(version, commit, date, tofuVersion string) string {
	return fmt.Sprintf(
		"Nebari Infrastructure Core (NIC)\n"+
			"Version: %s\n"+
			"Commit: %s\n"+
			"Built: %s\n"+
			"OpenTofu version: %s\n",
		version, commit, date, tofuVersion,
	)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the version information for Nebari Infrastructure Core (NIC).`,
	RunE:  runVersion,
}

func runVersion(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.version")
	defer span.End()

	slog.Info("Version command executed", "version", version, "commit", commit, "date", date)

	fmt.Print(versionString(version, commit, date, tofu.Version))

	client, err := nic.NewClient(ctx)
	if err != nil {
		return err
	}
	providers := client.ProviderNames(ctx)
	fmt.Printf("Registered cloud providers: %v\n", providers.Cluster)
	fmt.Printf("Registered DNS providers: %v\n", providers.DNS)

	return nil
}
