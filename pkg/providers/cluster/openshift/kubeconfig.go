package openshift

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Provision-mode kubeconfig acquisition.
//
// Unlike EKS (where the aws provider builds a stateless kubeconfig with an
// `aws eks get-token` exec block), ROSA HCP has no equivalent token plugin. The
// proven path — encoded from the Phase A manual run — is:
//
//  1. `rosa create admin` to add a cluster-admin via the htpasswd IDP,
//  2. poll `oc login` until the IDP propagates and the credential activates
//     (can take many minutes on a fresh cluster), then
//  3. capture the kubeconfig `oc login` writes.
//
// This requires the `rosa` and `oc` CLIs on PATH and a valid RHCS_TOKEN in the
// environment (already mandated by provision-mode Validate). break-glass
// credentials would avoid the IDP wait but require the cluster be created with
// external authentication, a larger change deferred for now.
const (
	// adminUsername is the htpasswd user `rosa create admin` provisions.
	adminUsername = "cluster-admin"
	// adminLoginActivationTimeout bounds how long we poll `oc login` for the
	// freshly created admin credential to become active.
	adminLoginActivationTimeout = 25 * time.Minute
	// adminLoginPollInterval is the wait between `oc login` attempts.
	adminLoginPollInterval = 20 * time.Second
	// rosaCmd / ocCmd are the external binaries the provision path shells out to.
	rosaCmd = "rosa"
	ocCmd   = "oc"
)

// ocLoginRe extracts the API URL, username, and password from the `oc login ...`
// line that `rosa create admin` prints. Parsing the human-readable line keeps us
// independent of the rosa JSON output schema across CLI versions.
var ocLoginRe = regexp.MustCompile(`oc login (\S+) --username (\S+) --password (\S+)`)

// GetKubeconfig returns a kubeconfig for the OpenShift cluster.
//
// In existing mode it loads the configured kubeconfig file and filters it to the
// selected context (mirroring the existing provider). In provision mode it
// derives a cluster-admin kubeconfig from the ROSA cluster via the rosa/oc CLIs.
//
// Results are cached in-memory per Provider instance.
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.GetKubeconfig")
	defer span.End()

	cfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("mode", cfg.Mode()),
		attribute.String("project_name", projectName),
	)

	switch cfg.Mode() {
	case ModeExisting:
		return p.existingKubeconfig(cfg)
	case ModeProvision:
		return p.provisionKubeconfig(ctx, projectName)
	default:
		return nil, fmt.Errorf("invalid openshift mode %q", cfg.Mode())
	}
}

// existingKubeconfig loads + filters the kubeconfig for the configured context,
// caching the serialized result.
func (p *Provider) existingKubeconfig(cfg *Config) ([]byte, error) {
	cacheKey := "existing:" + cfg.Context

	p.kubeconfigMu.RLock()
	if cached, ok := p.kubeconfigCache[cacheKey]; ok {
		p.kubeconfigMu.RUnlock()
		return cached, nil
	}
	p.kubeconfigMu.RUnlock()

	path, err := cfg.GetKubeconfigPath()
	if err != nil {
		return nil, err
	}
	data, err := kubeconfig.LoadFromPath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", path, err)
	}
	filtered, err := kubeconfig.FilterByContext(data, cfg.Context)
	if err != nil {
		return nil, err
	}
	out, err := kubeconfig.WriteBytes(filtered)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	p.kubeconfigMu.Lock()
	p.kubeconfigCache[cacheKey] = out
	p.kubeconfigMu.Unlock()
	return out, nil
}

// provisionKubeconfig derives a cluster-admin kubeconfig for a provisioned ROSA
// cluster, caching the result per Provider instance.
func (p *Provider) provisionKubeconfig(ctx context.Context, projectName string) ([]byte, error) {
	cacheKey := "provision:" + projectName

	p.kubeconfigMu.RLock()
	if cached, ok := p.kubeconfigCache[cacheKey]; ok {
		p.kubeconfigMu.RUnlock()
		return cached, nil
	}
	p.kubeconfigMu.RUnlock()

	if err := requireCLIs(rosaCmd, ocCmd); err != nil {
		return nil, err
	}

	apiURL, password, err := ensureClusterAdmin(ctx, projectName)
	if err != nil {
		return nil, err
	}

	out, err := pollOCLogin(ctx, apiURL, adminUsername, password)
	if err != nil {
		return nil, err
	}

	p.kubeconfigMu.Lock()
	p.kubeconfigCache[cacheKey] = out
	p.kubeconfigMu.Unlock()
	return out, nil
}

// requireCLIs returns a descriptive error if any required binary is missing from
// PATH, so provision mode fails fast with actionable guidance.
func requireCLIs(bins ...string) error {
	var missing []string
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			missing = append(missing, b)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("openshift provision mode requires these CLIs on PATH: %s (install the ROSA and OpenShift clients)", strings.Join(missing, ", "))
	}
	return nil
}

// ensureClusterAdmin (re)creates the cluster-admin htpasswd user and returns the
// cluster API URL and the generated password. If an admin already exists its
// password is unknown to us, so we delete and recreate to obtain a fresh one,
// keeping the operation idempotent across repeated deploys.
func ensureClusterAdmin(ctx context.Context, clusterName string) (apiURL, password string, err error) {
	out, err := runCmd(ctx, rosaCmd, "create", "admin", "--cluster", clusterName, "--yes")
	if err != nil && strings.Contains(strings.ToLower(out), "already") {
		// Existing admin's password is unrecoverable; recreate it.
		if _, derr := runCmd(ctx, rosaCmd, "delete", "admin", "--cluster", clusterName, "--yes"); derr != nil {
			return "", "", fmt.Errorf("failed to delete existing cluster admin: %w", derr)
		}
		out, err = runCmd(ctx, rosaCmd, "create", "admin", "--cluster", clusterName, "--yes")
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to create cluster admin: %w\n%s", err, out)
	}

	apiURL, _, password, perr := parseAdminLogin(out)
	if perr != nil {
		return "", "", fmt.Errorf("%w\n%s", perr, out)
	}
	return apiURL, password, nil
}

// parseAdminLogin extracts the API URL, username, and password from the
// `oc login ...` line emitted by `rosa create admin`.
func parseAdminLogin(output string) (apiURL, username, password string, err error) {
	m := ocLoginRe.FindStringSubmatch(output)
	if m == nil {
		return "", "", "", fmt.Errorf("could not parse 'oc login' credentials from rosa output")
	}
	return m[1], m[2], m[3], nil
}

// pollOCLogin repeatedly runs `oc login` until the credential activates (or the
// timeout/context expires), returning the kubeconfig oc writes on success.
func pollOCLogin(ctx context.Context, apiURL, username, password string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "nic-ocp-kubeconfig-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp kubeconfig: %w", err)
	}
	kubeconfigPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(kubeconfigPath) }()

	deadline := time.Now().Add(adminLoginActivationTimeout)
	status.Send(ctx, status.NewUpdate(status.LevelInfo,
		fmt.Sprintf("Waiting for cluster-admin login to activate at %s (up to %s)", apiURL, adminLoginActivationTimeout)).
		WithResource("kubeconfig").WithAction("login"))

	var lastOut string
	for {
		out, lerr := runCmd(ctx, ocCmd, "login", apiURL,
			"--username", username,
			"--password", password,
			"--kubeconfig", kubeconfigPath,
			"--request-timeout=20s",
		)
		if lerr == nil {
			data, rerr := os.ReadFile(kubeconfigPath)
			if rerr != nil {
				return nil, fmt.Errorf("login succeeded but failed to read kubeconfig: %w", rerr)
			}
			return data, nil
		}
		lastOut = out

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s waiting for cluster-admin login to activate: %w\n%s",
				adminLoginActivationTimeout, lerr, lastOut)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(adminLoginPollInterval):
		}
	}
}

// runCmd runs an external command, inheriting the parent environment (so rosa
// reads RHCS_TOKEN and AWS credentials), and returns its combined output.
func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err
}
