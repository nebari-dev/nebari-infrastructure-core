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
	if t == nil {
		return "", nil
	}
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
	if t == nil {
		return "", nil
	}
	pem, err := t.resolve()
	if err != nil {
		return "", err
	}
	if len(pem) == 0 {
		return "", nil
	}
	return base64.StdEncoding.EncodeToString(pem), nil
}

// Validate performs structural checks only and never touches disk, so it is safe
// in environments where a path:-based PEM isn't present (CI, config linting). It
// enforces mutual exclusion of path/inline and, for inline values (which are
// available without I/O), the PEM-marker checks. The file read and the same
// PEM-marker checks for path:-based bundles happen later at resolve time
// (ResolvePEM/ResolveBase64), called during deploy and destroy.
func (t *TrustBundleConfig) Validate() error {
	if t == nil {
		return nil
	}
	pathSet := strings.TrimSpace(t.Path) != ""
	inlineSet := strings.TrimSpace(t.Inline) != ""
	if pathSet && inlineSet {
		return errors.New("trust_bundle: only one of path or inline may be set")
	}
	if inlineSet {
		return checkPEM([]byte(t.Inline), "inline value")
	}
	return nil
}

// resolve reads and validates the bundle, returning the raw PEM bytes. Returns
// nil bytes (no error) when the bundle is unset.
func (t *TrustBundleConfig) resolve() ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	// Trim once and reuse: a whitespace-padded path must be treated identically
	// for the set-check and the read, or a value like "  /real/path  " is
	// detected as set but then fed verbatim to os.ReadFile and fails.
	path := strings.TrimSpace(t.Path)
	inlineSet := strings.TrimSpace(t.Inline) != ""
	if path != "" && inlineSet {
		return nil, errors.New("trust_bundle: only one of path or inline may be set")
	}
	if path == "" && !inlineSet {
		return nil, nil
	}

	var (
		pem     []byte
		subject string
	)
	if path != "" {
		subject = path
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("trust_bundle: read %s: %w", path, err)
		}
		pem = data
	} else {
		subject = "inline value"
		pem = []byte(t.Inline)
	}

	if err := checkPEM(pem, subject); err != nil {
		return nil, err
	}
	return pem, nil
}

// checkPEM verifies the bytes contain a PEM certificate and reject any private
// key block. The private-key guard is defense-in-depth: a resolved bundle is
// written to OpenTofu state and projected into every namespace via the GitOps
// repo, so a stray cert+key file must never be distributed cluster-wide or
// committed to git.
func checkPEM(pem []byte, subject string) error {
	s := string(pem)
	if !strings.Contains(s, "-----BEGIN CERTIFICATE-----") {
		return fmt.Errorf("trust_bundle: no PEM certificate found in %s", subject)
	}
	if strings.Contains(s, "PRIVATE KEY") {
		return fmt.Errorf("trust_bundle: %s contains a private key block; only certificates may be distributed", subject)
	}
	return nil
}
