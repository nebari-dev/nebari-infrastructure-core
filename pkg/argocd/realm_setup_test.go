package argocd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// fakeKcadm stands in for kcadm.sh against a nebari realm left by an earlier
// deploy: every create fails the way a duplicate does, get returns the objects
// that already exist, and each call is appended to $KCADM_LOG.
// KCADM_HIDE_CLIENT drops one clientId from the client lookup, and
// KCADM_FAIL_UPDATE_OF makes the update of one client id fail.
const fakeKcadm = `#!/bin/sh
printf '%s\n' "$*" >> "$KCADM_LOG"
case "$1 $2" in
create*) exit 1 ;;
"get clients")
  printf '[ '
  [ "$KCADM_HIDE_CLIENT" = argocd ] || printf '{\n  "id" : "argocd-uuid",\n  "clientId" : "argocd"\n}, '
  [ "$KCADM_HIDE_CLIENT" = longhorn ] || printf '{\n  "id" : "longhorn-uuid",\n  "clientId" : "longhorn"\n}, '
  printf '{\n  "id" : "account-uuid",\n  "clientId" : "account"\n} ]\n' ;;
"get client-scopes")
  printf '[ {\n  "id" : "groups-uuid",\n  "name" : "groups"\n} ]\n' ;;
"get users")
  printf '[ {\n  "id" : "admin-uuid"\n} ]\n' ;;
"get groups")
  printf '[ {\n  "id" : "argocd-admins-uuid",\n  "name" : "argocd-admins"\n}, {\n  "id" : "longhorn-admins-uuid",\n  "name" : "longhorn-admins"\n} ]\n' ;;
"update clients/"*)
  [ -n "$KCADM_FAIL_UPDATE_OF" ] && [ "$2" = "clients/$KCADM_FAIL_UPDATE_OF" ] && exit 1 ;;
esac
exit 0
`

// kcadm calls the realm-setup script makes when rendered for
// realmSetupTestDomain and run against fakeKcadm.
const (
	realmSetupTestDomain   = "new.example.com"
	clientLookup           = "get clients -r nebari --fields id,clientId"
	argocdRedirectUpdate   = `update clients/argocd-uuid -r nebari -s redirectUris=["https://argocd.new.example.com/auth/callback"]`
	longhornRedirectUpdate = `update clients/longhorn-uuid -r nebari -s redirectUris=["https://longhorn.new.example.com/oauth2/callback"]`
)

// TestRealmSetupReconcilesExistingClients runs the rendered realm-setup script
// against a realm whose argocd and longhorn clients already exist. The create
// calls fail there, so only an explicit update moves each redirect URI to the
// current domain (#463).
func TestRealmSetupReconcilesExistingClients(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}

	tests := []struct {
		name                  string
		withoutLonghorn       bool     // render the job with Longhorn disabled
		withoutLonghornSecret bool     // leave LONGHORN_CLIENT_SECRET unset
		env                   []string // added to the script's environment
		wantFail              bool
		wantOutput            string   // substring of the script's output
		wantCalls             []string // kcadm calls that must be made
		wantNoCalls           []string // kcadm calls that must not be made
		wantLastCall          string   // where a failing script must stop
	}{
		{
			name:      "existing clients get the current domain's redirect URIs",
			wantCalls: []string{argocdRedirectUpdate, longhornRedirectUpdate},
		},
		{
			name:            "without Longhorn only the argocd client is reconciled",
			withoutLonghorn: true,
			wantCalls:       []string{argocdRedirectUpdate},
			wantNoCalls:     []string{longhornRedirectUpdate},
		},
		{
			name:                  "a missing Longhorn client secret still skips the Longhorn client",
			withoutLonghornSecret: true,
			wantOutput:            "skipping Longhorn OIDC client setup",
			wantCalls:             []string{argocdRedirectUpdate},
			wantNoCalls:           []string{longhornRedirectUpdate},
		},
		{
			name:         "a failed argocd client update fails the job",
			env:          []string{"KCADM_FAIL_UPDATE_OF=argocd-uuid"},
			wantFail:     true,
			wantLastCall: argocdRedirectUpdate,
		},
		{
			name:         "a failed longhorn client update fails the job",
			env:          []string{"KCADM_FAIL_UPDATE_OF=longhorn-uuid"},
			wantFail:     true,
			wantLastCall: longhornRedirectUpdate,
		},
		{
			name:         "an argocd client missing after create fails the job",
			env:          []string{"KCADM_HIDE_CLIENT=argocd"},
			wantFail:     true,
			wantOutput:   "ArgoCD OIDC client not found after create",
			wantLastCall: clientLookup,
		},
		{
			name:         "a longhorn client missing after create fails the job",
			env:          []string{"KCADM_HIDE_CLIENT=longhorn"},
			wantFail:     true,
			wantOutput:   "Longhorn OIDC client not found after create",
			wantLastCall: clientLookup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, env := renderRealmSetupScript(t, realmSetupTestDomain, !tt.withoutLonghorn)
			if tt.withoutLonghornSecret {
				// The kubelet leaves an optional secretKeyRef unset when its Secret is missing.
				env = slices.DeleteFunc(env, func(kv string) bool {
					return strings.HasPrefix(kv, "LONGHORN_CLIENT_SECRET=")
				})
			}
			out, calls, err := runWithFakeKcadm(t, bash, script, append(env, tt.env...))

			if tt.wantFail {
				if err == nil {
					t.Fatalf("realm-setup script succeeded, want failure\noutput:\n%s", out)
				}
				if len(calls) == 0 || calls[len(calls)-1] != tt.wantLastCall {
					t.Errorf("realm-setup script did not stop at\n  %s\ncalls:\n  %s", tt.wantLastCall, strings.Join(calls, "\n  "))
				}
			} else if err != nil {
				t.Fatalf("realm-setup script failed: %v\noutput:\n%s", err, out)
			}
			if !strings.Contains(out, tt.wantOutput) {
				t.Errorf("realm-setup output does not contain %q\noutput:\n%s", tt.wantOutput, out)
			}
			for _, want := range tt.wantCalls {
				if !slices.Contains(calls, want) {
					t.Errorf("kcadm was not called with\n  %s\ncalls:\n  %s", want, strings.Join(calls, "\n  "))
				}
			}
			for _, unwanted := range tt.wantNoCalls {
				if slices.Contains(calls, unwanted) {
					t.Errorf("kcadm was called with\n  %s", unwanted)
				}
			}
		})
	}
}

// renderRealmSetupScript renders the realm-setup Job for domain and returns
// its shell script and container environment. Variables fed from Secrets get
// placeholder values.
func renderRealmSetupScript(t *testing.T, domain string, longhorn bool) (string, []string) {
	t.Helper()
	workDir := t.TempDir()
	cfg := &config.NebariConfig{Domain: domain}
	settings := cluster.InfraSettings{LonghornEnabled: longhorn}
	if err := WriteAllToGit(context.Background(), workDir, cfg, nil, settings, ""); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(workDir, "manifests", "keycloak", "realm-setup-job.yaml")) //nolint:gosec // path is t.TempDir() + constant
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := yaml.Unmarshal(raw, &job); err != nil {
		t.Fatalf("parse realm-setup job: %v", err)
	}
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 || len(containers[0].Command) != 3 || containers[0].Command[1] != "-c" {
		t.Fatalf("realm-setup job is not a single bash -c container: %+v", containers)
	}
	var env []string
	for _, e := range containers[0].Env {
		value := e.Value
		if e.ValueFrom != nil {
			value = "test-" + strings.ToLower(e.Name)
		}
		env = append(env, e.Name+"="+value)
	}
	return containers[0].Command[2], env
}

// runWithFakeKcadm points the script's KCADM at fakeKcadm, runs it with bash
// and env, and returns its combined output, the kcadm calls it made in order,
// and its exit error.
func runWithFakeKcadm(t *testing.T, bash, script string, env []string) (string, []string, error) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "kcadm.sh")
	if err := os.WriteFile(stub, []byte(fakeKcadm), 0o700); err != nil { //nolint:gosec // G306: the stub must be executable
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "kcadm.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	const kcadmLine = `KCADM="/opt/keycloak/bin/kcadm.sh"`
	if strings.Count(script, kcadmLine) != 1 {
		t.Fatalf("realm-setup script does not set %s exactly once", kcadmLine)
	}
	script = strings.Replace(script, kcadmLine, `KCADM="`+stub+`"`, 1)

	cmd := exec.Command(bash, "-c", script) //nolint:gosec // G204: runs the repository's own rendered script
	cmd.Env = append(slices.Clone(env), "PATH="+os.Getenv("PATH"), "KCADM_LOG="+logPath)
	out, runErr := cmd.CombinedOutput()

	log, err := os.ReadFile(logPath) //nolint:gosec // path is t.TempDir() + constant
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	if trimmed := strings.TrimSpace(string(log)); trimmed != "" {
		calls = strings.Split(trimmed, "\n")
	}
	return string(out), calls, runErr
}
