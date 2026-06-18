package argocd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// PreflightGatewayTLS validates user-supplied certificate material for the
// files and env sources before any cluster mutation or GitOps commit, so a bad
// path, unset env var, or mismatched keypair fails the deploy fast instead of
// silently rendering a gateway that can never terminate TLS.
//
// It is a no-op for non-existing certificate types and for the existing_secret
// source (which is validated against the live cluster during ConfigureGatewayTLS).
// No certificate or key material is logged.
func PreflightGatewayTLS(cfg *config.NebariConfig) error {
	cert := cfg.Certificate
	if cert == nil || cert.Type != config.CertificateTypeExisting {
		return nil
	}

	var certPEM, keyPEM []byte
	var err error
	switch {
	case cert.Files != nil:
		certPEM, keyPEM, err = readCertFiles(cert.Files)
	case cert.Env != nil:
		certPEM, keyPEM, err = readCertEnv(cert.Env)
	default:
		// existing_secret is validated against the cluster later.
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("invalid TLS certificate/key pair: %w", err)
	}
	return nil
}

// ConfigureGatewayTLS handles user-supplied TLS certificates
// (certificate.type=existing). For the files and env sources it reads the PEM
// material and creates a kubernetes.io/tls secret directly in
// envoy-gateway-system. For the existing_secret source it fetches the
// already-present secret. For every source it parses the resolved certificate
// and warns (never fails) when a recommended SAN is missing.
//
// Certificate and key material is never logged or recorded in span attributes.
// For non-existing certificate types this is a no-op.
func ConfigureGatewayTLS(ctx context.Context, client kubernetes.Interface, cfg *config.NebariConfig) error {
	cert := cfg.Certificate
	if cert == nil || cert.Type != config.CertificateTypeExisting {
		return nil
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.ConfigureGatewayTLS")
	defer span.End()

	switch {
	case cert.Files != nil:
		span.SetAttributes(attribute.String("source", "files"))
		certPEM, keyPEM, err := readCertFiles(cert.Files)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return createGatewayTLSSecret(ctx, client, cfg, certPEM, keyPEM)
	case cert.Env != nil:
		span.SetAttributes(attribute.String("source", "env"))
		certPEM, keyPEM, err := readCertEnv(cert.Env)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return createGatewayTLSSecret(ctx, client, cfg, certPEM, keyPEM)
	case cert.ExistingSecret != nil:
		span.SetAttributes(attribute.String("source", "existing_secret"))
		checkExistingSecretSANs(ctx, client, cfg)
		return nil
	default:
		// Validation guarantees exactly one source; defensive no-op.
		return nil
	}
}

// readCertFiles reads PEM cert/key material from disk.
func readCertFiles(files *config.CertFiles) (certPEM, keyPEM []byte, err error) {
	certPEM, err = os.ReadFile(files.CertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read certificate file %q: %w", files.CertFile, err)
	}
	keyPEM, err = os.ReadFile(files.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read key file %q: %w", files.KeyFile, err)
	}
	return certPEM, keyPEM, nil
}

// readCertEnv reads raw (non-base64) PEM cert/key material from environment variables.
func readCertEnv(env *config.CertEnv) (certPEM, keyPEM []byte, err error) {
	certStr := os.Getenv(env.CertEnv)
	if certStr == "" {
		return nil, nil, fmt.Errorf("certificate env var %q is empty or unset", env.CertEnv)
	}
	keyStr := os.Getenv(env.KeyEnv)
	if keyStr == "" {
		return nil, nil, fmt.Errorf("key env var %q is empty or unset", env.KeyEnv)
	}
	return []byte(certStr), []byte(keyStr), nil
}

// createGatewayTLSSecret validates the keypair, warns on missing SANs, and
// creates (or updates) the kubernetes.io/tls secret in envoy-gateway-system.
func createGatewayTLSSecret(ctx context.Context, client kubernetes.Interface, cfg *config.NebariConfig, certPEM, keyPEM []byte) error {
	// Fail fast on a malformed or mismatched keypair.
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("invalid TLS certificate/key pair: %w", err)
	}

	warnOnMissingSANs(ctx, certPEM, cfg.Domain)

	name := cfg.Certificate.ResolvedSecretName()
	namespace := config.DefaultGatewayTLSNamespace

	// The gateway expects the secret in envoy-gateway-system, which may not
	// exist yet (envoy-gateway is installed via Argo CD afterwards).
	if err := createNamespace(ctx, client, namespace); err != nil {
		return fmt.Errorf("ensure namespace %s: %w", namespace, err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "nebari-gateway",
				"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Configuring user-supplied gateway TLS secret").
		WithResource("certificate").
		WithAction("configuring").
		WithMetadata("secret", name).
		WithMetadata("namespace", namespace))

	_, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if _, createErr := client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); createErr != nil {
			return fmt.Errorf("create gateway TLS secret %s/%s: %w", namespace, name, createErr)
		}
	} else if _, updateErr := client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{}); updateErr != nil {
		return fmt.Errorf("update gateway TLS secret %s/%s: %w", namespace, name, updateErr)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Gateway TLS secret configured from user-supplied certificate").
		WithResource("certificate").
		WithAction("configured").
		WithMetadata("secret", name).
		WithMetadata("namespace", namespace))
	return nil
}

// checkExistingSecretSANs fetches a user-managed TLS secret and warns when the
// certificate is missing recommended SANs. Failures are non-fatal: the secret
// may be created out-of-band, and SAN coverage is advisory.
func checkExistingSecretSANs(ctx context.Context, client kubernetes.Interface, cfg *config.NebariConfig) {
	name, namespace := cfg.Certificate.GatewaySecretRef()

	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"Could not fetch user-supplied TLS secret to verify SANs; ensure it exists before traffic is served").
			WithMetadata("secret", name).
			WithMetadata("namespace", namespace).
			WithMetadata("error", err.Error()))
		return
	}

	certPEM, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("TLS secret %s/%s has no %s key", namespace, name, corev1.TLSCertKey)).
			WithMetadata("secret", name).
			WithMetadata("namespace", namespace))
		return
	}
	warnOnMissingSANs(ctx, certPEM, cfg.Domain)
}

// warnOnMissingSANs warns (does not fail) when the certificate does not cover
// the apex domain or the keycloak/argocd subdomains. Hard-failing would block
// legitimate setups whose certs carry extra hostnames we don't track.
func warnOnMissingSANs(ctx context.Context, certPEM []byte, domain string) {
	if domain == "" {
		return
	}
	missing, err := missingSANs(certPEM, domain)
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"Could not parse user-supplied TLS certificate to verify SANs").
			WithMetadata("error", err.Error()))
		return
	}
	if len(missing) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"User-supplied TLS certificate is missing recommended SANs; some hostnames may fail TLS").
			WithMetadata("missing_hosts", strings.Join(missing, ", ")))
	}
}

// missingSANs parses certPEM and returns the subset of the required gateway
// hostnames (apex, keycloak.<domain>, argocd.<domain>) the certificate does not
// cover. Wildcard SANs are honored via x509.Certificate.VerifyHostname.
func missingSANs(certPEM []byte, domain string) ([]string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	required := []string{domain, "keycloak." + domain, "argocd." + domain}
	var missing []string
	for _, host := range required {
		if cert.VerifyHostname(host) != nil {
			missing = append(missing, host)
		}
	}
	return missing, nil
}
