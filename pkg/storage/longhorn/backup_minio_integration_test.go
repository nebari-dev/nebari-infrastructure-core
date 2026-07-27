//go:build integration

package longhorn

import (
	"context"
	"os"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// minioBackupConfig builds a Longhorn backup config that targets an in-cluster
// MinIO (S3-compatible, 3rd-party) endpoint. This is the configuration shape a
// user supplies for a self-hosted object store: an explicit Endpoint plus
// VirtualHostedStyle, which Longhorn requires for non-AWS S3 backends.
func minioBackupConfig() *config.LonghornBackupConfig {
	return &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket:             "longhorn-backups",
			Region:             "us-east-1",
			Prefix:             "clusterA/",
			Endpoint:           "http://minio.minio.svc:9000",
			VirtualHostedStyle: true,
			AccessKeyIDEnv:     "LONGHORN_S3_ACCESS_KEY_ID",
			SecretAccessKeyEnv: "LONGHORN_S3_SECRET_ACCESS_KEY",
		},
	}
}

// TestIntegration_LonghornMinIO_CredentialAndTargetMapping exercises the pure
// config -> credential-Secret-data and config -> BackupTarget-URL mapping for a
// MinIO / 3rd-party S3 endpoint. It runs WITHOUT a cluster (the fake client is
// only needed because ResolveCredentials takes a kubernetes.Interface, and no
// ca_cert is referenced here so the client is never consulted).
//
// This is the part of the MinIO backup story that can be asserted
// deterministically in CI: that a self-hosted S3 endpoint produces the
// AWS_ENDPOINTS + VIRTUAL_HOSTED_STYLE Secret keys Longhorn needs, and that the
// BackupTarget URL is the s3://<bucket>@<region>/<prefix> form.
func TestIntegration_LonghornMinIO_CredentialAndTargetMapping(t *testing.T) {
	const (
		accessKey = "minioadmin"
		secretKey = "minioadmin-secret"
	)
	t.Setenv("LONGHORN_S3_ACCESS_KEY_ID", accessKey)
	t.Setenv("LONGHORN_S3_SECRET_ACCESS_KEY", secretKey)

	cfg := minioBackupConfig()
	client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but fine for tests

	creds, err := ResolveCredentials(context.Background(), client, cfg)
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.AccessKeyID != accessKey {
		t.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, accessKey)
	}
	if creds.SecretAccessKey != secretKey {
		t.Errorf("SecretAccessKey = %q, want %q", creds.SecretAccessKey, secretKey)
	}

	data := CredentialSecretData(cfg, creds)
	want := map[string]string{
		"AWS_ACCESS_KEY_ID":     accessKey,
		"AWS_SECRET_ACCESS_KEY": secretKey,
		"AWS_ENDPOINTS":         "http://minio.minio.svc:9000",
		"VIRTUAL_HOSTED_STYLE":  "true",
	}
	for k, v := range want {
		if got := data[k]; got != v {
			t.Errorf("CredentialSecretData[%q] = %q, want %q", k, got, v)
		}
	}
	// A 3rd-party endpoint must NOT inject an AWS_CERT key when no ca_cert is
	// configured.
	if _, ok := data["AWS_CERT"]; ok {
		t.Errorf("CredentialSecretData unexpectedly set AWS_CERT for a config with no ca_cert")
	}

	if got, want := cfg.BackupTargetURL(), "s3://longhorn-backups@us-east-1/clusterA/"; got != want {
		t.Errorf("BackupTargetURL() = %q, want %q", got, want)
	}
}

// TestIntegration_LonghornMinIO_BuildCredentialSecret confirms the higher-level
// BuildCredentialSecret helper wires the same MinIO mapping into a ready-to-apply
// corev1.Secret in the Longhorn namespace. Still no cluster required.
func TestIntegration_LonghornMinIO_BuildCredentialSecret(t *testing.T) {
	t.Setenv("LONGHORN_S3_ACCESS_KEY_ID", "minioadmin")
	t.Setenv("LONGHORN_S3_SECRET_ACCESS_KEY", "minioadmin-secret")

	cfg := minioBackupConfig()
	//nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but fine for tests
	secret, err := BuildCredentialSecret(context.Background(), fake.NewSimpleClientset(), cfg)
	if err != nil {
		t.Fatalf("BuildCredentialSecret: %v", err)
	}
	if secret.Name != BackupCredentialSecretName {
		t.Errorf("secret name = %q, want %q", secret.Name, BackupCredentialSecretName)
	}
	if secret.Namespace != Namespace {
		t.Errorf("secret namespace = %q, want %q", secret.Namespace, Namespace)
	}
	if got := secret.StringData["AWS_ENDPOINTS"]; got != "http://minio.minio.svc:9000" {
		t.Errorf("secret AWS_ENDPOINTS = %q, want the MinIO endpoint", got)
	}
	if got := secret.StringData["VIRTUAL_HOSTED_STYLE"]; got != "true" {
		t.Errorf("secret VIRTUAL_HOSTED_STYLE = %q, want %q", got, "true")
	}
}

// requireCluster gates the live round-trip on a usable Kubernetes cluster. It
// follows the same kubeconfig-loading path as the rest of the codebase
// (clientcmd.RESTConfigFromKubeConfig, see pkg/nic/deploy.go) and additionally
// requires LONGHORN_INTEGRATION_LIVE=1 as an explicit opt-in so the destructive
// backup/restore round-trip never runs by accident. When the prerequisites are
// absent it t.Skip()s with the documented manual steps rather than failing.
//
// It returns a connected kubernetes.Interface for the live test to use.
func requireCluster(t *testing.T) kubernetes.Interface {
	t.Helper()

	if os.Getenv("LONGHORN_INTEGRATION_LIVE") != "1" {
		t.Skip(liveSkipMessage)
	}
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		t.Skip("LONGHORN_INTEGRATION_LIVE=1 but KUBECONFIG is unset; " + liveSkipMessage)
	}
	kubeconfigBytes, err := os.ReadFile(kubeconfigPath) //nolint:gosec // G304: kubeconfig path is operator-supplied test input
	if err != nil {
		t.Skipf("cannot read KUBECONFIG %q: %v; %s", kubeconfigPath, err, liveSkipMessage)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		t.Skipf("invalid KUBECONFIG: %v; %s", err, liveSkipMessage)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		t.Skipf("cannot build kube client: %v; %s", err, liveSkipMessage)
	}
	// Connectivity check: a live cluster must answer a server-version request.
	if _, err := client.Discovery().ServerVersion(); err != nil {
		t.Skipf("cluster not reachable: %v; %s", err, liveSkipMessage)
	}
	return client
}

const liveSkipMessage = `skipping live Longhorn+MinIO backup/restore round-trip.
To run it, point KUBECONFIG at a cluster that has Longhorn installed and set
LONGHORN_INTEGRATION_LIVE=1. Manual round-trip steps the live test follows:
  1. Deploy MinIO in-cluster with known creds; create the backup bucket.
  2. export LONGHORN_S3_ACCESS_KEY_ID / LONGHORN_S3_SECRET_ACCESS_KEY.
  3. Apply the credential Secret (longhorn.BuildCredentialSecret) plus the
     BackupTarget / RecurringJobs / Setting manifests (or the deploy subset).
  4. Create a PVC on the "longhorn" StorageClass and write known data.
  5. Trigger/await the default-daily-backup; assert a Backup CR appears in the
     BackupTarget.
  6. Restore into a new volume and assert the known data matches.`

// TestIntegration_LonghornMinIO_BackupRestoreRoundTrip is the live round-trip.
// It SKIPS cleanly unless a real cluster with Longhorn is available and the
// LONGHORN_INTEGRATION_LIVE opt-in is set (see requireCluster). The round-trip
// itself drives Longhorn's CRDs (BackupTarget, RecurringJob, Backup, Volume)
// which live outside the typed client-go scheme; wiring those dynamic clients is
// intentionally left to a follow-up once a Longhorn-equipped CI cluster exists,
// rather than fabricating it here. The documented sequence is in liveSkipMessage
// and inline below.
func TestIntegration_LonghornMinIO_BackupRestoreRoundTrip(t *testing.T) {
	client := requireCluster(t)
	_ = client

	// Live round-trip (executed only when requireCluster did not skip):
	//
	//   ctx := context.Background()
	//
	//   // 1. MinIO is expected to be deployed in-cluster (minio.minio.svc:9000)
	//   //    with the bucket already created by the test harness.
	//   cfg := minioBackupConfig()
	//
	//   // 2. Credentials come from the environment (set by the harness).
	//   //
	//   // 3. Apply the credential Secret and the BackupTarget/RecurringJob/Setting.
	//   secret, err := BuildCredentialSecret(ctx, client, cfg)
	//   // ...apply secret via client.CoreV1().Secrets(Namespace).Apply(...)
	//   // ...apply BackupTarget(cfg.BackupTargetURL(), BackupCredentialSecretName)
	//   //    and the RecurringJobs / cluster Setting via a dynamic client.
	//
	//   // 4. Create a PVC on StorageClassName, mount it, write known bytes.
	//   // 5. Trigger the backup RecurringJob (or create an on-demand Backup CR),
	//   //    poll the BackupTarget until a Backup CR is Completed.
	//   // 6. Create a Volume from the Backup, bind a PVC, read it back, and
	//   //    assert the bytes match what step 4 wrote.
	//
	// See liveSkipMessage for the operator-facing version of these steps.
	t.Skip("live backup/restore body not yet implemented; requires Longhorn CRD dynamic clients on a live cluster")
}
