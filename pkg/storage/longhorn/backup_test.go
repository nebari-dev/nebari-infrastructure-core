package longhorn

import (
	"testing"

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
