//go:build ocp_e2e

// Live, money-spending end-to-end test for the OpenShift provision path.
//
// It provisions a real ROSA HCP cluster, verifies the provisioned kubeconfig
// reaches the API, then destroys everything (cluster + S3 state bucket). It is
// gated behind the `ocp_e2e` build tag so it never runs in normal `go test`.
//
// Run:
//
//	source ../../../../../.rosa-session.env   # RHCS_TOKEN + AWS creds in env
//	go test -tags ocp_e2e -run TestProvisionLifecycleE2E -timeout 90m -v \
//	    ./pkg/providers/cluster/openshift/
package openshift

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ocGetNodes writes the kubeconfig to a temp file and runs `oc get nodes`,
// proving the provisioned credential actually reaches the cluster API.
func ocGetNodes(ctx context.Context, kc []byte) (string, error) {
	f, err := os.CreateTemp("", "ocp-e2e-kc-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(kc); err != nil {
		return "", err
	}
	_ = f.Close()
	out, err := exec.CommandContext(ctx, "oc", "--kubeconfig", f.Name(), "get", "nodes").CombinedOutput()
	return string(out), err
}

// TestProvisionDryRunE2E is the cheap smoke test: it downloads OpenTofu, inits
// the rhcs+aws providers, and runs `tofu plan` against a local backend. It
// creates NO cloud resources, so it validates the template/init/plan wiring
// before committing to the ~15 min apply in TestProvisionLifecycleE2E.
func TestProvisionDryRunE2E(t *testing.T) {
	region := envOr("OCP_E2E_REGION", "us-east-1")
	project := envOr("OCP_E2E_NAME", "nic-e2e")

	ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
		fmt.Fprintf(os.Stderr, "STATUS %+v\n", u)
	})
	defer cleanup()

	cc := &config.ClusterConfig{Providers: map[string]any{"openshift": map[string]any{
		"mode":               "provision",
		"region":             region,
		"availability_zones": []string{region + "a"},
		"compute":            map[string]any{"instance_type": "m5.xlarge", "replicas": 2},
	}}}
	p := NewProvider()

	if err := p.Validate(ctx, project, cc); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := p.Deploy(ctx, project, cc, cluster.DeployOptions{DryRun: true}); err != nil {
		t.Fatalf("Deploy(dry-run): %v", err)
	}
	t.Log("dry-run plan succeeded — template/init/plan wiring is good")
}

// TestProvisionOnlyE2E provisions a ROSA cluster and LEAVES IT UP, writing the
// kubeconfig, kube-context, and apps domain to files for a follow-on existing-mode
// `nic deploy`. Tear down afterwards with TestDestroyOnlyE2E.
func TestProvisionOnlyE2E(t *testing.T) {
	region := envOr("OCP_E2E_REGION", "us-east-1")
	project := envOr("OCP_E2E_NAME", "nic-e2e")
	outDir := envOr("OCP_E2E_OUTDIR", os.TempDir())

	ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
		fmt.Fprintf(os.Stderr, "STATUS %+v\n", u)
	})
	defer cleanup()

	cc := &config.ClusterConfig{Providers: map[string]any{"openshift": map[string]any{
		"mode":               "provision",
		"region":             region,
		"availability_zones": []string{region + "a"},
		"compute":            map[string]any{"instance_type": "m5.xlarge", "replicas": 2},
	}}}
	p := NewProvider()

	if err := p.Validate(ctx, project, cc); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("PROVISION: standing up %q (~15 min)...", project)
	if err := p.Deploy(ctx, project, cc, cluster.DeployOptions{}); err != nil {
		t.Fatalf("Deploy(provision): %v", err)
	}

	kc, err := p.GetKubeconfig(ctx, project, cc)
	if err != nil {
		t.Fatalf("GetKubeconfig: %v", err)
	}
	kcPath := outDir + "/." + project + ".kubeconfig"
	if err := os.WriteFile(kcPath, kc, 0600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	ctxName, _ := exec.CommandContext(ctx, "oc", "--kubeconfig", kcPath, "config", "current-context").Output()
	domain, _ := exec.CommandContext(ctx, "oc", "--kubeconfig", kcPath, "get",
		"ingresses.config.openshift.io", "cluster", "-o", "jsonpath={.spec.domain}").Output()
	_ = os.WriteFile(outDir+"/."+project+".context", ctxName, 0600)
	_ = os.WriteFile(outDir+"/."+project+".appsdomain", domain, 0600)

	t.Logf("PROVISIONED. kubeconfig=%s", kcPath)
	t.Logf("  context=%s", string(ctxName))
	t.Logf("  appsDomain=%s", string(domain))
	t.Logf("  -> nebari domain would be: nebari.%s", string(domain))
}

// TestDestroyOnlyE2E runs Destroy against existing remote state. Use it to clean
// up a cluster left behind by a failed lifecycle run (e.g. a VPC-dependency
// teardown race) once the cluster has finished uninstalling.
func TestDestroyOnlyE2E(t *testing.T) {
	region := envOr("OCP_E2E_REGION", "us-east-1")
	project := envOr("OCP_E2E_NAME", "nic-e2e")

	ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
		fmt.Fprintf(os.Stderr, "STATUS %+v\n", u)
	})
	defer cleanup()

	cc := &config.ClusterConfig{Providers: map[string]any{"openshift": map[string]any{
		"mode":               "provision",
		"region":             region,
		"availability_zones": []string{region + "a"},
		"compute":            map[string]any{"instance_type": "m5.xlarge", "replicas": 2},
	}}}

	if err := NewProvider().Destroy(ctx, project, cc, cluster.DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	t.Log("destroy completed")
}

func TestProvisionLifecycleE2E(t *testing.T) {
	region := envOr("OCP_E2E_REGION", "us-east-1")
	project := envOr("OCP_E2E_NAME", "nic-e2e") // ROSA cluster name: <=15 chars

	// Stream provider/tofu progress to the test log so the ~15 min apply is visible.
	ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
		fmt.Fprintf(os.Stderr, "STATUS %+v\n", u)
	})
	defer cleanup()

	cc := &config.ClusterConfig{Providers: map[string]any{"openshift": map[string]any{
		"mode":               "provision",
		"region":             region,
		"availability_zones": []string{region + "a"},
		"compute":            map[string]any{"instance_type": "m5.xlarge", "replicas": 2},
	}}}
	p := NewProvider()

	if err := p.Validate(ctx, project, cc); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Guarantee teardown even if a later step fails midway. Destroy is
	// idempotent (no-op when nothing exists), so a double-destroy is safe.
	destroyed := false
	defer func() {
		if destroyed {
			return
		}
		t.Log("CLEANUP: destroying cluster + state bucket")
		if err := p.Destroy(ctx, project, cc, cluster.DestroyOptions{Force: true}); err != nil {
			t.Errorf("CLEANUP Destroy failed — MANUAL CLEANUP REQUIRED for cluster %q: %v", project, err)
		}
	}()

	t.Logf("PROVISION: standing up ROSA HCP cluster %q in %s (~15 min)...", project, region)
	if err := p.Deploy(ctx, project, cc, cluster.DeployOptions{}); err != nil {
		t.Fatalf("Deploy(provision): %v", err)
	}

	kc, err := p.GetKubeconfig(ctx, project, cc)
	if err != nil {
		t.Fatalf("GetKubeconfig: %v", err)
	}
	if len(kc) == 0 {
		t.Fatal("GetKubeconfig returned empty kubeconfig")
	}

	out, err := ocGetNodes(ctx, kc)
	if err != nil {
		t.Fatalf("oc get nodes via provisioned kubeconfig failed: %v\n%s", err, out)
	}
	t.Logf("VERIFY: cluster reachable. nodes:\n%s", out)

	t.Log("DESTROY: tearing down cluster + state bucket...")
	if err := p.Destroy(ctx, project, cc, cluster.DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	destroyed = true

	// Confirm the cluster is actually gone.
	descOut, descErr := exec.CommandContext(ctx, "rosa", "describe", "cluster", "-c", project).CombinedOutput()
	if descErr == nil {
		t.Errorf("cluster %q still exists after Destroy:\n%s", project, descOut)
	} else {
		t.Logf("VERIFY: cluster no longer described (expected): %v", descErr)
	}
}
