package main

import (
	"github.com/spf13/cobra"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
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
	ctx := cmd.Context()
	r := renderer.FromContext(ctx)

	providers := getValidNames(ctx, reg)
	r.Version(version, commit, tofu.Version, providers.ClusterProviders, providers.DNSProviders)

	return nil
}
