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
// When set at the top level of NebariConfig, the bundle is propagated into the
// cluster via trust-manager (the in-pod half of the trust-bundle propagation) on
// every provider. Only the AWS provider also installs it into worker-node OS
// trust stores; on other providers the bundle reaches pods only.
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

// Validate performs structural checks only and never touches disk, so it is safe
// in environments where a path:-based PEM isn't present (CI, config linting). It
// enforces mutual exclusion of path/inline and, for inline values (which are
// available without I/O), full certificate parsing. The file read and the same
// parsing for path:-based bundles happen later in Resolve, called during deploy
// and destroy.
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

// ErrInvalidTrustBundle wraps every content error from Resolve and Validate:
// no certificate, a block trust-manager would reject, an unparseable
// certificate, or private-key material. It does not wrap file read errors.
var ErrInvalidTrustBundle = errors.New("invalid trust_bundle")

// parseCertificates decodes every PEM block in raw and returns the X.509
// certificates it contains. It fails when there is no CERTIFICATE block, when
// any block is not a CERTIFICATE or carries PEM headers, when any CERTIFICATE
// block is not a parseable X.509 certificate, when a block's base64 is corrupt,
// or when the input carries private-key material.
//
// The block rules match trust-manager's AddCertsFromPEM, which rejects the
// whole source on a non-CERTIFICATE block or a block with headers; accepting
// them here would pass `nic validate` and then fail at trust-manager sync.
// Free text between blocks is still skipped, as both pem.Decode and
// trust-manager do. The private-key guard is defense-in-depth: a resolved
// bundle is written to OpenTofu state and projected into every namespace via
// the GitOps repo, so a stray cert+key file must never be distributed
// cluster-wide or committed to git. It runs on the raw text rather than only
// on decoded block types so a corrupted key block that pem.Decode would skip is
// still caught.
func parseCertificates(raw []byte, subject string) ([]*x509.Certificate, error) {
	if strings.Contains(string(raw), "PRIVATE KEY") {
		return nil, fmt.Errorf("%w: %s contains a private key block; only certificates may be distributed", ErrInvalidTrustBundle, subject)
	}

	var (
		certs  []*x509.Certificate
		blocks int
	)
	rest := raw
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		blocks++
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("%w: block %d in %s is a %q block; only CERTIFICATE blocks are permitted", ErrInvalidTrustBundle, blocks, subject, block.Type)
		}
		if len(block.Headers) != 0 {
			return nil, fmt.Errorf("%w: block %d in %s has PEM headers, which are not permitted", ErrInvalidTrustBundle, blocks, subject)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: block %d in %s is not a valid X.509 certificate: %w%s", ErrInvalidTrustBundle, blocks, subject, err, negativeSerialHint(err))
		}
		certs = append(certs, cert)
	}
	// pem.Decode silently skips a block whose base64 is corrupt, so a bundle
	// with one bad block among good ones would otherwise pass with that
	// certificate missing.
	if markers := strings.Count(string(raw), "-----BEGIN "); markers != blocks {
		return nil, fmt.Errorf("%w: %s has %d PEM BEGIN markers but only %d decodable blocks; check for corrupt base64 or a missing END line", ErrInvalidTrustBundle, subject, markers, blocks)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: no PEM certificate found in %s", ErrInvalidTrustBundle, subject)
	}
	return certs, nil
}

// negativeSerialHint points operators at the GODEBUG escape hatch when a CA
// fails to parse only because of a negative serial number, which Go has
// rejected by default since 1.23. trust-manager is built with the same default
// and would reject the certificate too, so the fix is to reissue the CA; the
// GODEBUG setting only lets nic itself get past the check (for example to
// destroy a deployment that already carries the certificate).
func negativeSerialHint(err error) string {
	if !strings.Contains(err.Error(), "negative serial number") {
		return ""
	}
	return " (Go rejects CAs with a negative serial number since 1.23, and so does trust-manager; reissue the CA, or set GODEBUG=x509negativeserial=1 to let nic accept it)"
}
