package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// TrustBundleConfig specifies the source of an extra CA bundle. Exactly one of
// Path or Inline must be set. Path is a filesystem path to a PEM file on the
// operator's machine; Inline is the PEM text itself.
//
// When set at the top level of NebariConfig, the bundle is propagated both to
// worker-node OS trust stores (via the cluster provider) and into the cluster
// via trust-manager (the in-pod half of
// https://github.com/nebari-dev/nebari-infrastructure-core/issues/307).
type TrustBundleConfig struct {
	Path   string `yaml:"path,omitempty"`
	Inline string `yaml:"inline,omitempty"`
}

// ResolvePEM returns the configured CA bundle as raw PEM text. Returns an empty
// string when the bundle is unset.
func (t *TrustBundleConfig) ResolvePEM() (string, error) {
	pem, err := t.resolve()
	if err != nil {
		return "", err
	}
	return string(pem), nil
}

// ResolveBase64 returns the configured CA bundle as a base64-encoded PEM string,
// suitable for passing straight to the terraform-aws-eks-cluster module's
// extra_ca_bundle input. Returns an empty string when the bundle is unset.
func (t *TrustBundleConfig) ResolveBase64() (string, error) {
	pem, err := t.resolve()
	if err != nil {
		return "", err
	}
	if len(pem) == 0 {
		return "", nil
	}
	return base64.StdEncoding.EncodeToString(pem), nil
}

// Validate checks that the bundle is well-formed without returning its contents.
// Safe to call at validate time so misconfigurations surface before deploy.
func (t *TrustBundleConfig) Validate() error {
	_, err := t.resolve()
	return err
}

// resolve reads and validates the bundle, returning the raw PEM bytes. Returns
// nil bytes (no error) when the bundle is unset.
func (t *TrustBundleConfig) resolve() ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	pathSet := strings.TrimSpace(t.Path) != ""
	inlineSet := strings.TrimSpace(t.Inline) != ""
	if pathSet && inlineSet {
		return nil, errors.New("trust_bundle: only one of path or inline may be set")
	}
	if !pathSet && !inlineSet {
		return nil, nil
	}

	var (
		pem     []byte
		subject string
	)
	if pathSet {
		subject = t.Path
		data, err := os.ReadFile(t.Path)
		if err != nil {
			return nil, fmt.Errorf("trust_bundle: read %s: %w", t.Path, err)
		}
		pem = data
	} else {
		subject = "inline value"
		pem = []byte(t.Inline)
	}

	if !strings.Contains(string(pem), "-----BEGIN CERTIFICATE-----") {
		return nil, fmt.Errorf("trust_bundle: no PEM certificate found in %s", subject)
	}
	return pem, nil
}
