package config

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// TrustBundleConfig specifies the source of an extra CA bundle. At most one of
// Path or Inline may be set; leaving both unset (or omitting the block) installs
// no bundle. Path is a filesystem path to a PEM file on the operator's machine;
// Inline is the PEM text itself.
//
// When set at the top level of NebariConfig, the bundle is propagated both to
// worker-node OS trust stores (via the cluster provider) and into the cluster
// via trust-manager (the in-pod half of the trust-bundle propagation).
type TrustBundleConfig struct {
	Path   string `yaml:"path,omitempty"`
	Inline string `yaml:"inline,omitempty"`
}

// ResolvedTrustBundle is the outcome of resolving a TrustBundleConfig: the PEM
// text plus enough provenance for the deploy path to tell the operator what was
// picked up. The zero value means no bundle is configured.
type ResolvedTrustBundle struct {
	// PEM is the raw bundle text, byte-for-byte as configured. Empty when unset.
	PEM string
	// Source is the trimmed file path for a path:-based bundle, or "inline".
	// Empty when unset.
	Source string
	// Certificates is the number of CERTIFICATE blocks that parsed as X.509.
	Certificates int
}

// IsSet reports whether a bundle was configured and resolved.
func (r ResolvedTrustBundle) IsSet() bool { return r.PEM != "" }

// Base64 returns the PEM base64-encoded, suitable for passing straight to a
// module input such as terraform-aws-eks-cluster's extra_ca_bundle. Empty when
// unset.
func (r ResolvedTrustBundle) Base64() string {
	if r.PEM == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(r.PEM))
}

// Resolve reads (for Path) and validates the configured bundle. It returns the
// zero ResolvedTrustBundle with no error when the bundle is unset.
func (t *TrustBundleConfig) Resolve() (ResolvedTrustBundle, error) {
	if t == nil {
		return ResolvedTrustBundle{}, nil
	}
	// Trim once and reuse: a whitespace-padded path must be treated identically
	// for the set-check and the read, or a value like "  /real/path  " is
	// detected as set but then fed verbatim to os.ReadFile and fails.
	path := strings.TrimSpace(t.Path)
	inlineSet := strings.TrimSpace(t.Inline) != ""
	if path != "" && inlineSet {
		return ResolvedTrustBundle{}, errors.New("trust_bundle: only one of path or inline may be set")
	}
	if path == "" && !inlineSet {
		return ResolvedTrustBundle{}, nil
	}

	var (
		raw    []byte
		source string
	)
	if path != "" {
		source = path
		data, err := os.ReadFile(path)
		if err != nil {
			return ResolvedTrustBundle{}, fmt.Errorf("trust_bundle: read %s: %w", path, err)
		}
		raw = data
	} else {
		source = "inline"
		raw = []byte(t.Inline)
	}

	certs, err := parseCertificates(raw, source)
	if err != nil {
		return ResolvedTrustBundle{}, err
	}
	return ResolvedTrustBundle{PEM: string(raw), Source: source, Certificates: len(certs)}, nil
}

// ResolvePEM returns the configured CA bundle as raw PEM text. Returns an empty
// string when the bundle is unset.
func (t *TrustBundleConfig) ResolvePEM() (string, error) {
	r, err := t.Resolve()
	if err != nil {
		return "", err
	}
	return r.PEM, nil
}

// ResolveBase64 returns the configured CA bundle as a base64-encoded PEM string,
// suitable for passing straight to the terraform-aws-eks-cluster module's
// extra_ca_bundle input. Returns an empty string when the bundle is unset.
func (t *TrustBundleConfig) ResolveBase64() (string, error) {
	r, err := t.Resolve()
	if err != nil {
		return "", err
	}
	return r.Base64(), nil
}

// Validate performs structural checks only and never touches disk, so it is safe
// in environments where a path:-based PEM isn't present (CI, config linting). It
// enforces mutual exclusion of path/inline and, for inline values (which are
// available without I/O), full certificate parsing. The file read and the same
// parsing for path:-based bundles happen later at resolve time (Resolve /
// ResolvePEM / ResolveBase64), called during deploy and destroy.
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
		_, err := parseCertificates([]byte(t.Inline), "inline value")
		return err
	}
	return nil
}

// parseCertificates decodes every PEM block in raw and returns the X.509
// certificates it contains. It fails when there is no CERTIFICATE block, when
// any CERTIFICATE block is not a parseable X.509 certificate, or when the input
// carries private-key material.
//
// Non-certificate blocks (e.g. EC PARAMETERS) and free text between blocks are
// skipped, matching what update-ca-certificates and trust-manager do with the
// same bytes. The private-key guard is defense-in-depth: a resolved bundle is
// written to OpenTofu state and projected into every namespace via the GitOps
// repo, so a stray cert+key file must never be distributed cluster-wide or
// committed to git. It runs on the raw text rather than only on decoded block
// types so a corrupted key block that pem.Decode would skip is still caught.
func parseCertificates(raw []byte, subject string) ([]*x509.Certificate, error) {
	if strings.Contains(string(raw), "PRIVATE KEY") {
		return nil, fmt.Errorf("trust_bundle: %s contains a private key block; only certificates may be distributed", subject)
	}

	var certs []*x509.Certificate
	rest := raw
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("trust_bundle: block %d in %s is not a valid X.509 certificate: %w", len(certs)+1, subject, err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("trust_bundle: no PEM certificate found in %s", subject)
	}
	return certs, nil
}
