package cli

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/quickstart"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

var (
	quickstartOut       string
	quickstartDomain    string
	quickstartHTTPPort  int
	quickstartHTTPSPort int
	quickstartForce     bool
	quickstartDryRun    bool

	quickstartCmd = &cobra.Command{
		Use:   "quickstart",
		Short: "Deploy Nebari on a local kind cluster with Nebi and the Data Science Pack",
		Long: `Deploy Nebari on a local kind cluster, with Nebi and the Data Science Pack
installed on top.

quickstart writes a regular NIC config file (config.yaml by default) and
deploys it. Afterwards, edit that file and run 'nic deploy' to change the
cluster, or run 'nic destroy' to delete it.

The software packs are written into the local GitOps repository, where ArgoCD
installs them. Edit or delete those files to change or remove the packs.

Use --dry-run to write the config file without deploying.`,
		RunE: runQuickstart,
	}
)

func init() {
	quickstartCmd.Flags().StringVarP(&quickstartOut, "out", "o", "config.yaml", "Path to write the generated config file to")
	quickstartCmd.Flags().StringVar(&quickstartDomain, "domain", quickstart.DefaultDomain, "Domain the platform is served on")
	quickstartCmd.Flags().IntVar(&quickstartHTTPPort, "http-port", 0, "Host port for HTTP (default 80)")
	quickstartCmd.Flags().IntVar(&quickstartHTTPSPort, "https-port", 0, "Host port for HTTPS (default 443)")
	quickstartCmd.Flags().BoolVar(&quickstartForce, "force", false, "Overwrite the config file if it already exists with different content")
	quickstartCmd.Flags().BoolVar(&quickstartDryRun, "dry-run", false, "Write the config file without deploying")
}

func runQuickstart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.quickstart")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", quickstartOut),
		attribute.String("domain", quickstartDomain),
		attribute.Bool("dry_run", quickstartDryRun),
	)

	content, err := quickstart.RenderConfig(quickstart.Options{
		Domain:    quickstartDomain,
		HTTPPort:  quickstartHTTPPort,
		HTTPSPort: quickstartHTTPSPort,
	})
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := writeQuickstartConfig(quickstartOut, content, quickstartForce); err != nil {
		span.RecordError(err)
		return err
	}
	slog.Info("Wrote config file", "path", quickstartOut)

	cfg, err := config.ParseConfig(ctx, quickstartOut)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if quickstartDryRun {
		names, err := quickstart.PackNames()
		if err != nil {
			span.RecordError(err)
			return err
		}
		fmt.Printf("\nDry run: wrote %s and deployed nothing.\n", quickstartOut)
		fmt.Printf("Running without --dry-run deploys it and installs: %s\n\n", strings.Join(names, ", "))
		return nil
	}

	packs, err := quickstart.RenderPacks(cfg.Domain)
	if err != nil {
		span.RecordError(err)
		return err
	}

	client, err := nic.NewClient(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}

	ctx, cleanup := nic.StartSlogHandler(ctx, slog.Default())
	defer cleanup()

	result, err := client.Deploy(ctx, cfg, nic.DeployOptions{})
	if err != nil {
		span.RecordError(err)
		return err
	}

	repoDir := config.DefaultLocalRepositoryPath(cfg.ProjectName)
	if err := quickstart.InstallPacks(ctx, repoDir, packs); err != nil {
		span.RecordError(err)
		return fmt.Errorf("install software packs: %w", err)
	}
	slog.Info("Added software packs to the GitOps repository", "path", repoDir)

	// ArgoCD picks the packs up at its next poll anyway, so a failed refresh
	// only delays them.
	if kubeconfig, err := client.Kubeconfig(ctx, cfg); err != nil {
		slog.Warn("Could not refresh ArgoCD, packs will sync at the next poll", "error", err)
	} else if err := quickstart.RefreshRoot(ctx, kubeconfig); err != nil {
		slog.Warn("Could not refresh ArgoCD, packs will sync at the next poll", "error", err)
	}

	cleanup()

	printDeployResult(cfg, result, false)
	printQuickstartSummary(cfg, result, quickstartOut)
	return nil
}

// writeQuickstartConfig writes content to path. An existing file with the same
// content is left alone, so re-running the quickstart with the same flags
// works; an existing file with different content is only replaced with force.
func writeQuickstartConfig(path string, content []byte, force bool) error {
	existing, err := os.ReadFile(path) //nolint:gosec // operator-chosen output path
	switch {
	case err == nil:
		if bytes.Equal(existing, content) {
			return nil
		}
		if !force {
			return fmt.Errorf("%s already exists with different content: pass --out to write elsewhere, or --force to overwrite it", path)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil { //nolint:gosec // config holds no secrets
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func printQuickstartSummary(cfg *config.NebariConfig, result *nic.DeployResult, configPath string) {
	// Config discovery finds ./config.yaml on its own; anything else needs -f.
	fileFlag := ""
	if configPath != "config.yaml" {
		fileFlag = " -f " + configPath
	}

	port := ""
	if result.LBEndpoint != nil && result.LBEndpoint.Port != 0 && result.LBEndpoint.Port != 443 {
		port = fmt.Sprintf(":%d", result.LBEndpoint.Port)
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  QUICKSTART")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  ArgoCD is installing the software packs. This takes several minutes.")
	fmt.Println("  Watch progress with:")
	fmt.Println()
	fmt.Println("    kubectl get applications -n argocd")
	fmt.Println()
	fmt.Println("  Once they are healthy:")
	fmt.Println()
	fmt.Printf("    Landing page: https://%s%s\n", cfg.Domain, port)
	fmt.Printf("    JupyterHub:   https://hub.%s%s\n", cfg.Domain, port)
	fmt.Printf("    Nebi:         https://nebi.%s%s\n", cfg.Domain, port)
	fmt.Println()
	fmt.Println("  Sign-in credentials:")
	fmt.Println()
	fmt.Printf("    nic outputs --show-secrets%s\n", fileFlag)
	fmt.Println()
	fmt.Println("  To delete the cluster:")
	fmt.Println()
	fmt.Printf("    nic destroy%s\n", fileFlag)
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}
