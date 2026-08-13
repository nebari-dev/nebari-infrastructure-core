package hetzner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// EnvHetznerK3sPath points NIC at a pre-installed hetzner-k3s binary, bypassing
// PATH discovery and download entirely. It mirrors NIC_TOFU_PATH for OpenTofu.
const EnvHetznerK3sPath = "NIC_HETZNER_K3S_PATH"

// binaryName is the hetzner-k3s executable looked up on PATH.
const binaryName = "hetzner-k3s"

// windowsOS is the runtime.GOOS value for Windows, where file modes carry no
// executable bit.
const windowsOS = "windows"

// Source identifies how the hetzner-k3s binary in use was obtained.
type Source string

const (
	// SourceOverride means the binary came from an explicit NIC_HETZNER_K3S_PATH.
	SourceOverride Source = "override"
	// SourcePath means the binary was discovered as `hetzner-k3s` on PATH.
	SourcePath Source = "path"
	// SourceDownload means NIC downloaded its own pinned binary.
	SourceDownload Source = "download"
)

// ResolvedBinary describes the hetzner-k3s binary NIC resolved and where it
// came from. Version is empty for external binaries: unlike OpenTofu,
// hetzner-k3s is a single pinned release with no supported range and no
// self-version probe wired, so NIC does not gate external binaries on version
// (see resolve).
type ResolvedBinary struct {
	Path    string
	Version string
	Source  Source
}

// Resolution is the outcome of external-binary resolution. Like the OpenTofu
// resolver, it is a pure query: it never emits status updates, so callers
// decide how to surface Notes.
type Resolution struct {
	// Binary is the external binary to use, or nil when NIC should fall back
	// to downloading its pinned version.
	Binary *ResolvedBinary
	// Notes are human-readable diagnostics produced while resolving.
	Notes []string
}

// resolver finds an external hetzner-k3s binary. Its dependencies are injectable
// so resolution can be tested without a filesystem or PATH.
type resolver struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	stat     func(string) (os.FileInfo, error)
}

func newResolver() *resolver {
	return &resolver{
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		stat:     os.Stat,
	}
}

// ResolveExternal reports the pre-installed hetzner-k3s binary NIC would use, if
// any. The resolution order mirrors the OpenTofu resolver: NIC_HETZNER_K3S_PATH
// (hard error if unusable), then `hetzner-k3s` on PATH. A Resolution with a nil
// Binary means no external binary applies and NIC falls back to downloading its
// pinned version.
//
// Unlike OpenTofu, an external hetzner-k3s binary is NOT integrity-checked or
// version-verified: hetzner-k3s is not on conda-forge/prefix.dev, so there is no
// packaged alternative to normalize against, and the SHA256 table in binary.go
// only covers the pinned download. Pointing NIC at an external binary is an
// explicit trade of that verification for air-gapped/pre-provisioned installs;
// docs/operations/packaging.md states this plainly.
func ResolveExternal(ctx context.Context) (*Resolution, error) {
	return newResolver().resolve(ctx)
}

// resolve implements the external-binary resolution order. See ResolveExternal.
func (r *resolver) resolve(ctx context.Context) (res *Resolution, err error) {
	_, span := otel.Tracer("nebari-infrastructure-core").Start(ctx, "hetzner.ResolveExternal")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
			return
		}
		outcome := &ResolvedBinary{Source: SourceDownload, Version: DefaultHetznerK3sVersion}
		if res.Binary != nil {
			outcome = res.Binary
		}
		span.SetAttributes(
			attribute.String("hetzner.k3s_resolution.source", string(outcome.Source)),
			attribute.String("hetzner.k3s_resolution.path", outcome.Path),
		)
	}()

	if override := strings.TrimSpace(r.getenv(EnvHetznerK3sPath)); override != "" {
		binary, oErr := r.resolveOverride(override)
		if oErr != nil {
			return nil, oErr
		}
		return &Resolution{Binary: binary}, nil
	}

	path, lookErr := r.lookPath(binaryName)
	if lookErr != nil {
		return &Resolution{}, nil
	}

	// No version gate: hetzner-k3s has no supported range and no self-version
	// probe, so a binary discovered on PATH is accepted as-is. A stale or wrong
	// build here is the operator's responsibility, the same trade the explicit
	// override makes.
	return &Resolution{Binary: &ResolvedBinary{Path: path, Source: SourcePath}}, nil
}

// resolveOverride validates an explicit NIC_HETZNER_K3S_PATH. Unlike PATH
// discovery, every failure is a hard error: the operator asked for this exact
// binary, so silently downloading a different one would mask the
// misconfiguration.
func (r *resolver) resolveOverride(override string) (*ResolvedBinary, error) {
	info, err := r.stat(override)
	if err != nil {
		return nil, fmt.Errorf("%s points to %s: %w", EnvHetznerK3sPath, override, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s points to %s, which is a directory, not an executable", EnvHetznerK3sPath, override)
	}
	if runtime.GOOS != windowsOS && info.Mode().Perm()&0111 == 0 {
		return nil, fmt.Errorf("%s points to %s, which is not executable", EnvHetznerK3sPath, override)
	}

	return &ResolvedBinary{Path: override, Source: SourceOverride}, nil
}

// announce reports which external hetzner-k3s binary is in use. Announcing is
// the consumer's job (resolveHetznerK3sBinary), not resolve's, so resolution
// stays a pure query. It is info-level and states plainly that an external
// binary is unverified.
func (b *ResolvedBinary) announce(ctx context.Context) {
	from := fmt.Sprintf("PATH (%s)", b.Path)
	if b.Source == SourceOverride {
		from = fmt.Sprintf("%s (%s)", EnvHetznerK3sPath, b.Path)
	}
	status.Infof(ctx, "Using hetzner-k3s from %s; NIC does not verify its version or integrity (pinned build is %s)", from, DefaultHetznerK3sVersion)
}

// resolveHetznerK3sBinary returns the path to the hetzner-k3s binary NIC should
// use: an explicit override or a `hetzner-k3s` on PATH when present, otherwise
// the pinned binary downloaded (and SHA256-verified) into cacheDir. An unusable
// override is a hard error, surfaced before any cloud call at the deploy/destroy
// sites that invoke this.
func resolveHetznerK3sBinary(ctx context.Context, cacheDir string) (string, error) {
	res, err := ResolveExternal(ctx)
	if err != nil {
		return "", err
	}
	if res.Binary != nil {
		res.Binary.announce(ctx)
		return res.Binary.Path, nil
	}
	for _, note := range res.Notes {
		status.Warningf(ctx, "%s", note)
	}

	downloader := &hetznerK3sDownloader{version: DefaultHetznerK3sVersion, cacheDir: cacheDir}
	return ensureBinary(ctx, cacheDir, DefaultHetznerK3sVersion, downloader)
}
