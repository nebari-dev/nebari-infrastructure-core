package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	goversion "github.com/hashicorp/go-version"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// EnvTofuPath is the environment variable that points NIC at a pre-installed
// OpenTofu binary, bypassing PATH discovery and download entirely.
const EnvTofuPath = "NIC_TOFU_PATH"

// MinVersion is the oldest external OpenTofu release NIC accepts. External
// binaries below this floor are rejected; see compatibleVersion.
const MinVersion = "1.11.3"

// maxVersionExclusive caps external binaries below the next major version,
// where breaking CLI or state format changes are fair game.
const maxVersionExclusive = "2.0.0"

// Source identifies how the OpenTofu binary in use was obtained.
type Source string

const (
	// SourceOverride means the binary came from an explicit NIC_TOFU_PATH.
	SourceOverride Source = "override"
	// SourcePath means the binary was discovered as `tofu` on PATH.
	SourcePath Source = "path"
	// SourceDownload means NIC downloaded its own pinned binary.
	SourceDownload Source = "download"
)

// ResolvedBinary describes the OpenTofu binary NIC resolved and where it came from.
type ResolvedBinary struct {
	Path    string
	Version string
	Source  Source
}

// binaryVersionTimeout bounds the `tofu version -json` probe of an external binary.
const binaryVersionTimeout = 30 * time.Second

// windowsOS is the runtime.GOOS value for Windows, where file modes carry no
// executable bit.
const windowsOS = "windows"

// resolver finds an external OpenTofu binary. Its dependencies are injectable
// so resolution can be tested without a filesystem, PATH, or subprocess.
type resolver struct {
	getenv        func(string) string
	lookPath      func(string) (string, error)
	stat          func(string) (os.FileInfo, error)
	binaryVersion func(ctx context.Context, path string) (string, error)
}

func newResolver() *resolver {
	return &resolver{
		getenv:        os.Getenv,
		lookPath:      exec.LookPath,
		stat:          os.Stat,
		binaryVersion: queryBinaryVersion,
	}
}

// ResolveExternal reports the pre-installed OpenTofu binary NIC would use, if
// any. The resolution order is: NIC_TOFU_PATH (hard error if unusable), then
// `tofu` on PATH (skipped with a warning if unusable). A nil result with a nil
// error means no external binary applies and NIC falls back to downloading its
// pinned version.
func ResolveExternal(ctx context.Context) (*ResolvedBinary, error) {
	return newResolver().resolve(ctx)
}

// resolve implements the external-binary resolution order. See ResolveExternal.
func (r *resolver) resolve(ctx context.Context) (resolved *ResolvedBinary, err error) {
	ctx, span := otel.Tracer("nebari-infrastructure-core").Start(ctx, "tofu.ResolveExternal")
	defer span.End()
	defer func() {
		if err != nil {
			span.SetAttributes(attribute.String("tofu.resolution.error", err.Error()))
			return
		}
		outcome := &ResolvedBinary{Source: SourceDownload, Version: Version}
		if resolved != nil {
			outcome = resolved
		}
		span.SetAttributes(
			attribute.String("tofu.resolution.source", string(outcome.Source)),
			attribute.String("tofu.resolution.version", outcome.Version),
			attribute.String("tofu.resolution.path", outcome.Path),
		)
	}()

	if override := r.getenv(EnvTofuPath); override != "" {
		return r.resolveOverride(ctx, override)
	}

	path, err := r.lookPath("tofu")
	if err != nil {
		return nil, nil
	}

	ver, err := r.binaryVersion(ctx, path)
	if err != nil {
		status.Warningf(ctx, "Ignoring tofu on PATH (%s): failed to determine its version: %v; falling back to download", path, err)
		return nil, nil
	}
	if err := compatibleVersion(ver); err != nil {
		status.Warningf(ctx, "Ignoring tofu %s on PATH (%s): %v; falling back to download", ver, path, err)
		return nil, nil
	}

	reportExternal(ctx, ver, fmt.Sprintf("PATH (%s)", path))
	return &ResolvedBinary{Path: path, Version: ver, Source: SourcePath}, nil
}

// resolveOverride validates an explicit NIC_TOFU_PATH. Unlike PATH discovery,
// every failure is a hard error: the operator asked for this exact binary, so
// silently downloading a different one would mask the misconfiguration.
func (r *resolver) resolveOverride(ctx context.Context, override string) (*ResolvedBinary, error) {
	info, err := r.stat(override)
	if err != nil {
		return nil, fmt.Errorf("%s points to %s: %w", EnvTofuPath, override, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s points to %s, which is a directory, not an executable", EnvTofuPath, override)
	}
	if runtime.GOOS != windowsOS && info.Mode().Perm()&0111 == 0 {
		return nil, fmt.Errorf("%s points to %s, which is not executable", EnvTofuPath, override)
	}

	ver, err := r.binaryVersion(ctx, override)
	if err != nil {
		return nil, fmt.Errorf("failed to determine version of %s binary %s: %w", EnvTofuPath, override, err)
	}
	if err := compatibleVersion(ver); err != nil {
		return nil, fmt.Errorf("%s binary %s: %w", EnvTofuPath, override, err)
	}

	reportExternal(ctx, ver, fmt.Sprintf("%s (%s)", EnvTofuPath, override))
	return &ResolvedBinary{Path: override, Version: ver, Source: SourceOverride}, nil
}

// reportExternal announces which external binary is in use, warning when its
// version differs from the pinned version NIC is tested against.
func reportExternal(ctx context.Context, ver, from string) {
	if ver == Version {
		status.Infof(ctx, "Using OpenTofu %s from %s", ver, from)
		return
	}
	status.Warningf(ctx, "Using OpenTofu %s from %s; NIC is tested against %s", ver, from, Version)
}

// compatibleVersion enforces the supported range [MinVersion, maxVersionExclusive)
// for external binaries.
func compatibleVersion(raw string) error {
	v, err := goversion.NewVersion(raw)
	if err != nil {
		return fmt.Errorf("unparseable OpenTofu version %q: %w", raw, err)
	}
	if v.Core().LessThan(goversion.Must(goversion.NewVersion(MinVersion))) {
		return fmt.Errorf("OpenTofu %s is below the minimum supported version %s", raw, MinVersion)
	}
	if v.Core().GreaterThanOrEqual(goversion.Must(goversion.NewVersion(maxVersionExclusive))) {
		return fmt.Errorf("OpenTofu %s is not supported (must be below %s)", raw, maxVersionExclusive)
	}
	return nil
}

// queryBinaryVersion runs `<path> version -json` and extracts the version.
// OpenTofu keeps the terraform_version JSON key for compatibility.
func queryBinaryVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, binaryVersionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "version", "-json").Output()
	if err != nil {
		return "", fmt.Errorf("failed to run %s version -json: %w", path, err)
	}

	var payload struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("failed to parse %s version -json output: %w", path, err)
	}
	if payload.TerraformVersion == "" {
		return "", fmt.Errorf("%s version -json reported no version", path)
	}

	return payload.TerraformVersion, nil
}
