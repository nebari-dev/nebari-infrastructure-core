package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/quickstart"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// dnsCheckTimeout bounds the lookup of the platform domain, so an offline
// machine or a slow resolver doesn't hold up the deploy.
const dnsCheckTimeout = 3 * time.Second

// kindGatewayAddress is where kind publishes the gateway, used for the
// /etc/hosts line when no deploy result carries the address (--dry-run).
const kindGatewayAddress = "127.0.0.1"

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
installs them. Edit or delete those files and commit the change to change or
remove the packs.

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

	// A failed check doesn't stop the deploy: the cluster is fine either way,
	// and the user can fix DNS while it runs. The warning is repeated after
	// the summary, where it can't scroll away under the deploy logs.
	dnsHost := "keycloak." + cfg.Domain
	dnsCtx, cancelDNS := context.WithTimeout(ctx, dnsCheckTimeout)
	dnsErr := quickstart.ResolvesToLoopback(dnsCtx, net.DefaultResolver, dnsHost)
	cancelDNS()
	if dnsErr != nil {
		slog.Warn("The platform domain doesn't resolve to this machine, so the browser can't reach the platform", "host", dnsHost, "error", dnsErr)
	}

	if quickstartDryRun {
		names, err := quickstart.PackNames()
		if err != nil {
			span.RecordError(err)
			return err
		}
		fmt.Printf("\nDry run: wrote %s and deployed nothing.\n", quickstartOut)
		fmt.Printf("Running without --dry-run deploys it and installs: %s\n\n", strings.Join(names, ", "))
		if dnsErr != nil {
			printDNSWarning(cfg, dnsHost, kindGatewayAddress)
		}
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
	// Deploy only warns when Argo CD fails to install. Without it nothing
	// syncs the platform or the packs, so return the error here.
	if !result.ArgoCDInstalled {
		err := errors.New("the Argo CD install failed, so the platform and software packs can't be deployed (see the warnings above)")
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

	// The summary replaces nic deploy's Argo CD, Keycloak and DNS boxes, which
	// describe steps a quickstart user doesn't need.
	printQuickstartSummary(cfg, result, quickstartOut, filepath.Join(repoDir, quickstart.AppsDir))
	if dnsErr != nil {
		gatewayAddress := kindGatewayAddress
		if result.LBEndpoint != nil && result.LBEndpoint.IP != "" {
			gatewayAddress = result.LBEndpoint.IP
		}
		printDNSWarning(cfg, dnsHost, gatewayAddress)
	}
	return nil
}

// printDNSWarning prints the failed DNS check as the last thing in the output,
// so it isn't lost above the summary. The default domain resolves through
// public DNS, so a failure there points at the local resolver. A custom domain
// may simply not be published anywhere.
func printDNSWarning(cfg *config.NebariConfig, host, gatewayAddress string) {
	hostsLine := gatewayAddress + " " + strings.Join(quickstart.Hostnames(cfg.Domain), " ")

	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  WARNING: YOUR BROWSER CAN'T REACH THE PLATFORM YET")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	if cfg.Domain == quickstart.DefaultDomain {
		fmt.Printf("  %s doesn't resolve to this machine, although it does\n", host)
		fmt.Println("  through public DNS. Your DNS resolver probably blocks public names that point")
		fmt.Println("  at your own machine. Fix it with one of:")
		fmt.Println()
		fmt.Println("    - switch this machine's DNS to 1.1.1.1 or 8.8.8.8")
		fmt.Println("    - turn on Secure DNS in your browser")
		fmt.Println("    - add this line to /etc/hosts:")
		fmt.Println()
		fmt.Printf("        %s\n", hostsLine)
	} else {
		fmt.Printf("  %s doesn't resolve to this machine. Point the domain's\n", host)
		fmt.Printf("  DNS records at %s, or add this line to /etc/hosts:\n", gatewayAddress)
		fmt.Println()
		fmt.Printf("    %s\n", hostsLine)
	}
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
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

func printQuickstartSummary(cfg *config.NebariConfig, result *nic.DeployResult, configPath, packsDir string) {
	port := ""
	if result.LBEndpoint != nil && result.LBEndpoint.Port != 0 && result.LBEndpoint.Port != 443 {
		port = fmt.Sprintf(":%d", result.LBEndpoint.Port)
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  QUICKSTART")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  ArgoCD is installing the software packs. This takes 10 to 15 minutes.")
	fmt.Println("  Watch progress with:")
	fmt.Println()
	fmt.Println("    kubectl get applications -n argocd")
	fmt.Println()
	fmt.Println("  The platform is ready when every app shows Healthy under HEALTH STATUS.")
	fmt.Println("  Until then, some apps show Missing or Progressing. This is expected.")
	fmt.Println()
	fmt.Println("  Then open:")
	fmt.Println()
	fmt.Printf("    Landing page: https://%s%s\n", cfg.Domain, port)
	fmt.Printf("    JupyterHub:   https://hub.%s%s\n", cfg.Domain, port)
	fmt.Printf("    Nebi:         https://nebi.%s%s\n", cfg.Domain, port)
	fmt.Println()
	fmt.Println("  Your browser will warn that the connection isn't private, because the")
	fmt.Println("  cluster signs its own certificates. This is expected for a local cluster.")
	fmt.Println("  Choose to continue (\"Advanced\", then \"Proceed\"), once for each app.")
	fmt.Println()
	fmt.Println("  The software packs are Argo CD Applications in the local GitOps repository:")
	fmt.Println()
	fmt.Printf("    %s\n", packsDir)
	fmt.Println()
	fmt.Println("  To change or remove a pack, edit or delete its file and commit the change")
	fmt.Println("  there. Argo CD only sees committed changes.")
	fmt.Println()
	fmt.Println("  Sign-in credentials:")
	fmt.Println()
	// Always pass -f: config discovery prefers NIC_CONFIG_PATH over
	// ./config.yaml, which could point these commands at another cluster.
	fmt.Printf("    nic outputs --show-secrets -f %s\n", configPath)
	fmt.Println()
	fmt.Println("  To delete the cluster:")
	fmt.Println()
	fmt.Printf("    nic destroy -f %s\n", configPath)
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}
