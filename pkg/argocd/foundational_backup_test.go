package argocd

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// A keyless target's Secret carries only AWS_IAM_ROLE_ARN — the mode switch
// Longhorn's credential gate requires. The usable credentials are injected
// into longhorn-manager pods by the Pod Identity webhook, which is the AWS
// provider's concern (aws.repairLonghornBackupPodIdentity), not this
// function's: no DaemonSet exists in this fake cluster and none is needed.
func TestCreateLonghornBackupSecretKeylessCarriesRoleARN(t *testing.T) {
	client := fake.NewSimpleClientset()
	backupCfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{Bucket: "b", Region: "us-east-1"},
	}

	err := createLonghornBackupSecret(context.Background(), client, backupCfg, "arn:aws:iam::123456789012:role/longhorn-backup")
	if err != nil {
		t.Fatalf("createLonghornBackupSecret() error = %v", err)
	}

	secret, err := client.CoreV1().Secrets(longhorn.Namespace).Get(
		context.Background(), longhorn.BackupCredentialSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("credential Secret was not applied: %v", err)
	}
	if got := getSecretValue(secret, "AWS_IAM_ROLE_ARN"); got == "" {
		t.Error("credential Secret is missing AWS_IAM_ROLE_ARN; Longhorn rejects a keyless secret without it")
	}
}

// A static-key target's Secret carries the credentials themselves, resolved
// from the configured environment variables.
func TestCreateLonghornBackupSecretStaticKeys(t *testing.T) {
	t.Setenv("TEST_LONGHORN_AK", "AKIAEXAMPLE")
	t.Setenv("TEST_LONGHORN_SK", "secret")

	client := fake.NewSimpleClientset()
	backupCfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket:             "b",
			Region:             "us-east-1",
			AccessKeyIDEnv:     "TEST_LONGHORN_AK",
			SecretAccessKeyEnv: "TEST_LONGHORN_SK",
		},
	}

	if err := createLonghornBackupSecret(context.Background(), client, backupCfg, ""); err != nil {
		t.Fatalf("createLonghornBackupSecret() error = %v", err)
	}

	secret, err := client.CoreV1().Secrets(longhorn.Namespace).Get(
		context.Background(), longhorn.BackupCredentialSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("credential Secret was not applied: %v", err)
	}
	if got := getSecretValue(secret, "AWS_ACCESS_KEY_ID"); got != "AKIAEXAMPLE" {
		t.Errorf("AWS_ACCESS_KEY_ID = %q, want value resolved from TEST_LONGHORN_AK", got)
	}
}
