package longhorn

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// BackupCredentialSecretName is the Secret (in Namespace) that Longhorn's
// BackupTarget references via credentialSecret.
const BackupCredentialSecretName = "longhorn-backup-credentials"

// Credentials holds the resolved (already read from env / cluster) secret
// values used to populate the Longhorn credential Secret.
type Credentials struct {
	// S3
	AccessKeyID     string
	SecretAccessKey string
	CACert          string // PEM; optional

	// Azure azblob
	AccountName string
	AccountKey  string
}

// CredentialSecretData maps the backup config + resolved credentials to the
// exact Secret data keys Longhorn expects (verified against the Longhorn
// "Set Backup Target" docs). Optional keys are omitted when empty.
func CredentialSecretData(cfg *config.LonghornBackupConfig, creds Credentials) map[string]string {
	data := map[string]string{}
	switch {
	case cfg.S3 != nil:
		data["AWS_ACCESS_KEY_ID"] = creds.AccessKeyID
		data["AWS_SECRET_ACCESS_KEY"] = creds.SecretAccessKey
		if cfg.S3.Endpoint != "" {
			data["AWS_ENDPOINTS"] = cfg.S3.Endpoint
		}
		if cfg.S3.VirtualHostedStyle {
			data["VIRTUAL_HOSTED_STYLE"] = "true"
		}
		if creds.CACert != "" {
			data["AWS_CERT"] = creds.CACert
		}
	case cfg.Azure != nil:
		data["AZBLOB_ACCOUNT_NAME"] = creds.AccountName
		data["AZBLOB_ACCOUNT_KEY"] = creds.AccountKey
		if cfg.Azure.Endpoint != "" {
			data["AZBLOB_ENDPOINT"] = cfg.Azure.Endpoint
		}
	}
	return data
}
