package longhorn

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestCredentialSecretDataS3(t *testing.T) {
	cfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket: "b", Region: "us-east-1",
			Endpoint:           "https://minio.example.com",
			VirtualHostedStyle: true,
		},
	}
	creds := Credentials{AccessKeyID: "AKIA", SecretAccessKey: "secret", CACert: "-----BEGIN CERTIFICATE-----"}
	got := CredentialSecretData(cfg, creds)

	want := map[string]string{
		"AWS_ACCESS_KEY_ID":     "AKIA",
		"AWS_SECRET_ACCESS_KEY": "secret",
		"AWS_ENDPOINTS":         "https://minio.example.com",
		"VIRTUAL_HOSTED_STYLE":  "true",
		"AWS_CERT":              "-----BEGIN CERTIFICATE-----",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestCredentialSecretDataS3Minimal(t *testing.T) {
	cfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{Bucket: "b", Region: "us-east-1"},
	}
	got := CredentialSecretData(cfg, Credentials{AccessKeyID: "AKIA", SecretAccessKey: "secret"})
	if _, ok := got["AWS_ENDPOINTS"]; ok {
		t.Error("AWS_ENDPOINTS should be omitted when endpoint empty")
	}
	if _, ok := got["VIRTUAL_HOSTED_STYLE"]; ok {
		t.Error("VIRTUAL_HOSTED_STYLE should be omitted when false")
	}
	if _, ok := got["AWS_CERT"]; ok {
		t.Error("AWS_CERT should be omitted when no CA cert")
	}
	if got["AWS_ACCESS_KEY_ID"] != "AKIA" || got["AWS_SECRET_ACCESS_KEY"] != "secret" {
		t.Errorf("missing access keys: %v", got)
	}
}

func TestCredentialSecretDataAzure(t *testing.T) {
	cfg := &config.LonghornBackupConfig{
		Azure: &config.AzureBackupTarget{Container: "c", StorageAccount: "sa", Endpoint: "https://compat.example.com"},
	}
	got := CredentialSecretData(cfg, Credentials{AccountName: "sa", AccountKey: "key=="})
	want := map[string]string{
		"AZBLOB_ACCOUNT_NAME": "sa",
		"AZBLOB_ACCOUNT_KEY":  "key==",
		"AZBLOB_ENDPOINT":     "https://compat.example.com",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestResolveCredentialsS3FromEnv(t *testing.T) {
	t.Setenv("TEST_AK", "AKIA")
	t.Setenv("TEST_SK", "secret")
	cfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket: "b", Region: "us-east-1",
			AccessKeyIDEnv: "TEST_AK", SecretAccessKeyEnv: "TEST_SK",
		},
	}
	creds, err := ResolveCredentials(context.Background(), fake.NewSimpleClientset(), cfg) //nolint:staticcheck
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.AccessKeyID != "AKIA" || creds.SecretAccessKey != "secret" {
		t.Fatalf("bad creds: %+v", creds)
	}
}

func TestResolveCredentialsMissingEnv(t *testing.T) {
	cfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket: "b", Region: "us-east-1",
			AccessKeyIDEnv: "DEFINITELY_UNSET_AK", SecretAccessKeyEnv: "DEFINITELY_UNSET_SK",
		},
	}
	_, err := ResolveCredentials(context.Background(), fake.NewSimpleClientset(), cfg) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
}

func TestResolveCredentialsCAFromSecret(t *testing.T) {
	t.Setenv("TEST_AK", "AKIA")
	t.Setenv("TEST_SK", "secret")
	client := fake.NewSimpleClientset(&corev1.Secret{ //nolint:staticcheck
		ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "longhorn-system"},
		Data:       map[string][]byte{"ca.crt": []byte("PEMDATA")},
	})
	cfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket: "b", Region: "us-east-1",
			AccessKeyIDEnv: "TEST_AK", SecretAccessKeyEnv: "TEST_SK",
			CACert: &config.CACertRef{Kind: "secret", Name: "ca", Namespace: "longhorn-system", Key: "ca.crt"},
		},
	}
	creds, err := ResolveCredentials(context.Background(), client, cfg)
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.CACert != "PEMDATA" {
		t.Fatalf("CA cert = %q, want PEMDATA", creds.CACert)
	}
}

func TestResolveCredentialsAzureFromEnv(t *testing.T) {
	t.Setenv("TEST_AN", "myaccount")
	t.Setenv("TEST_AKEY", "key==")
	cfg := &config.LonghornBackupConfig{
		Azure: &config.AzureBackupTarget{
			Container: "c", StorageAccount: "sa",
			AccountNameEnv: "TEST_AN", AccountKeyEnv: "TEST_AKEY",
		},
	}
	creds, err := ResolveCredentials(context.Background(), fake.NewSimpleClientset(), cfg) //nolint:staticcheck
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.AccountName != "myaccount" || creds.AccountKey != "key==" {
		t.Fatalf("bad creds: %+v", creds)
	}
}

func TestBuildCredentialSecret(t *testing.T) {
	t.Setenv("TEST_AK", "AKIA")
	t.Setenv("TEST_SK", "secret")
	cfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket: "b", Region: "us-east-1",
			AccessKeyIDEnv: "TEST_AK", SecretAccessKeyEnv: "TEST_SK",
		},
	}
	secret, err := BuildCredentialSecret(context.Background(), fake.NewSimpleClientset(), cfg) //nolint:staticcheck
	if err != nil {
		t.Fatalf("BuildCredentialSecret: %v", err)
	}
	if secret.Name != BackupCredentialSecretName || secret.Namespace != Namespace {
		t.Fatalf("bad metadata: %s/%s", secret.Namespace, secret.Name)
	}
	if secret.StringData["AWS_ACCESS_KEY_ID"] != "AKIA" {
		t.Fatalf("bad data: %v", secret.StringData)
	}
}
