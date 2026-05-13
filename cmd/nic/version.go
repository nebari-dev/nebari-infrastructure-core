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

const (
	version = "1.0.0"
	commit  = "dev"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the version information for Nebari Infrastructure Core (NIC).`,
	RunE:  runVersion,
}

func runVersion(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cmd.version")
	defer span.End()

	slog.Info("Version command executed", "version", version, "commit", commit)

	fmt.Printf("Nebari Infrastructure Core (NIC)\n")
	fmt.Printf("Version: %s\n", version)
	fmt.Printf("Commit: %s\n", commit)
	fmt.Printf("OpenTofu version: %s\n", tofu.Version)

	client, err := nic.NewClient()
	if err != nil {
		return err
	}
	providers := client.ProviderNames(ctx)
	fmt.Printf("Registered cloud providers: %v\n", providers.Cluster)
	fmt.Printf("Registered DNS providers: %v\n", providers.DNS)

	return nil
}
