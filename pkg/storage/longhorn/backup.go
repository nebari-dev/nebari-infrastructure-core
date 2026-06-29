package longhorn

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

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

func getenvRequired(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("env var name not configured")
	}
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("environment variable %s is not set or empty", name)
	}
	return v, nil
}

// ResolveCredentials reads the credential values from environment variables and,
// for S3 targets with a ca_cert reference, fetches the PEM from the referenced
// Secret/ConfigMap in the cluster.
func ResolveCredentials(ctx context.Context, client kubernetes.Interface, cfg *config.LonghornBackupConfig) (Credentials, error) {
	var creds Credentials
	switch {
	case cfg.S3 != nil:
		ak, err := getenvRequired(cfg.S3.AccessKeyIDEnv)
		if err != nil {
			return creds, fmt.Errorf("s3 access key: %w", err)
		}
		sk, err := getenvRequired(cfg.S3.SecretAccessKeyEnv)
		if err != nil {
			return creds, fmt.Errorf("s3 secret key: %w", err)
		}
		creds.AccessKeyID = ak
		creds.SecretAccessKey = sk
		if cfg.S3.CACert != nil {
			pem, err := fetchCACert(ctx, client, cfg.S3.CACert)
			if err != nil {
				return creds, fmt.Errorf("s3 ca_cert: %w", err)
			}
			creds.CACert = pem
		}
	case cfg.Azure != nil:
		an, err := getenvRequired(cfg.Azure.AccountNameEnv)
		if err != nil {
			return creds, fmt.Errorf("azure account name: %w", err)
		}
		ak, err := getenvRequired(cfg.Azure.AccountKeyEnv)
		if err != nil {
			return creds, fmt.Errorf("azure account key: %w", err)
		}
		creds.AccountName = an
		creds.AccountKey = ak
	default:
		return creds, fmt.Errorf("no backup target configured")
	}
	return creds, nil
}

func fetchCACert(ctx context.Context, client kubernetes.Interface, ref *config.CACertRef) (string, error) {
	ns := ref.Namespace
	if ns == "" {
		ns = Namespace
	}
	switch ref.Kind {
	case "secret":
		s, err := client.CoreV1().Secrets(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("read ca_cert secret %s/%s: %w", ns, ref.Name, err)
		}
		v, ok := s.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("ca_cert secret %s/%s has no key %q", ns, ref.Name, ref.Key)
		}
		return string(v), nil
	case "configmap":
		cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("read ca_cert configmap %s/%s: %w", ns, ref.Name, err)
		}
		v, ok := cm.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("ca_cert configmap %s/%s has no key %q", ns, ref.Name, ref.Key)
		}
		return v, nil
	default:
		return "", fmt.Errorf("unsupported ca_cert kind %q", ref.Kind)
	}
}

// BuildCredentialSecret resolves credentials and returns the corev1.Secret to
// apply into the Longhorn namespace. The caller applies it (create-or-update).
func BuildCredentialSecret(ctx context.Context, client kubernetes.Interface, cfg *config.LonghornBackupConfig) (*corev1.Secret, error) {
	creds, err := ResolveCredentials(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BackupCredentialSecretName,
			Namespace: Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
			},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: CredentialSecretData(cfg, creds),
	}, nil
}
