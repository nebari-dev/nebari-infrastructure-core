package tofu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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

// The supported range bounds, parsed once rather than on every
// compatibleVersion call.
var (
	minSupportedVersion   = goversion.Must(goversion.NewVersion(MinVersion))
	maxUnsupportedVersion = goversion.Must(goversion.NewVersion(maxVersionExclusive))
)

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

// Resolution is the outcome of external-binary resolution. Resolution itself
// is a pure query: it never emits status updates, so callers decide how to
// surface Notes (Setup sends them as warnings on the status channel; `nic
// version` folds them into its output line).
type Resolution struct {
	// Binary is the external binary to use, or nil when NIC should fall
	// back to downloading its pinned version.
	Binary *ResolvedBinary
	// Notes are human-readable diagnostics produced while resolving, e.g.
	// why a tofu found on PATH was ignored.
	Notes []string
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
// `tofu` on PATH (skipped with a note if unusable). A Resolution with a nil
// Binary means no external binary applies and NIC falls back to downloading
// its pinned version.
func ResolveExternal(ctx context.Context) (*Resolution, error) {
	return newResolver().resolve(ctx)
}

// resolve implements the external-binary resolution order. See ResolveExternal.
func (r *resolver) resolve(ctx context.Context) (res *Resolution, err error) {
	ctx, span := otel.Tracer("nebari-infrastructure-core").Start(ctx, "tofu.ResolveExternal")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
			return
		}
		outcome := &ResolvedBinary{Source: SourceDownload, Version: Version}
		if res.Binary != nil {
			outcome = res.Binary
		}
		span.SetAttributes(
			attribute.String("tofu.resolution.source", string(outcome.Source)),
			attribute.String("tofu.resolution.version", outcome.Version),
			attribute.String("tofu.resolution.path", outcome.Path),
		)
	}()

	if override := strings.TrimSpace(r.getenv(EnvTofuPath)); override != "" {
		binary, err := r.resolveOverride(ctx, override)
		if err != nil {
			return nil, err
		}
		return &Resolution{Binary: binary}, nil
	}

	path, lookErr := r.lookPath("tofu")
	if lookErr != nil {
		return &Resolution{}, nil
	}

	// An unusable PATH binary falls back to download instead of hard-erroring.
	// This is a deliberate divergence from the "hard error below the floor"
	// wording of issue #554: a stale system tofu on PATH predates NIC and must
	// not break existing users, so the hard error is reserved for the explicit
	// NIC_TOFU_PATH override, where the operator has stated intent.
	// The span event keeps "why did it download when tofu is on PATH?"
	// answerable from the trace, where the outcome alone reads source=download.
	ver, verErr := r.binaryVersion(ctx, path)
	if verErr != nil {
		span.AddEvent("tofu.path_binary_ignored", trace.WithAttributes(
			attribute.String("tofu.path", path),
			attribute.String("tofu.ignore_reason", verErr.Error()),
		))
		return &Resolution{Notes: []string{
			fmt.Sprintf("Ignoring tofu on PATH (%s): failed to determine its version: %v; falling back to download", path, verErr),
		}}, nil
	}
	if compatErr := compatibleVersion(ver); compatErr != nil {
		span.AddEvent("tofu.path_binary_ignored", trace.WithAttributes(
			attribute.String("tofu.path", path),
			attribute.String("tofu.version", ver),
			attribute.String("tofu.ignore_reason", compatErr.Error()),
		))
		return &Resolution{Notes: []string{
			fmt.Sprintf("Ignoring tofu %s on PATH (%s): %v; falling back to download", ver, path, compatErr),
		}}, nil
	}

	return &Resolution{Binary: &ResolvedBinary{Path: path, Version: ver, Source: SourcePath}}, nil
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

	return &ResolvedBinary{Path: override, Version: ver, Source: SourceOverride}, nil
}

// announce reports which external binary is in use, noting when its version
// differs from the pinned version NIC is tested against. Both messages are
// info-level: a different in-range version is the normal steady state for
// pixi/distro installs, so warning on every run would drown out warnings that
// need attention. Announcing is the consumer's job (Setup), not resolve's, so
// resolution stays a pure query that callers like `nic version` can render
// their own way without double-reporting.
func (b *ResolvedBinary) announce(ctx context.Context) {
	from := fmt.Sprintf("PATH (%s)", b.Path)
	if b.Source == SourceOverride {
		from = fmt.Sprintf("%s (%s)", EnvTofuPath, b.Path)
	}
	if b.Version == Version {
		status.Infof(ctx, "Using OpenTofu %s from %s", b.Version, from)
		return
	}
	status.Infof(ctx, "Using OpenTofu %s from %s; NIC is tested against %s", b.Version, from, Version)
}

// compatibleVersion enforces the supported range [MinVersion, maxVersionExclusive)
// for external binaries.
//
// Pre-release versions are deliberately compared by their base version (Core):
// 1.11.3-rc1 carries the features NIC needs from 1.11.3 and is accepted, while
// 2.0.0-beta1 previews the breaking changes of 2.0.0 and is rejected.
func compatibleVersion(raw string) error {
	v, err := goversion.NewVersion(raw)
	if err != nil {
		return fmt.Errorf("unparseable OpenTofu version %q: %w", raw, err)
	}
	if v.Core().LessThan(minSupportedVersion) {
		return fmt.Errorf("OpenTofu %s is below the minimum supported version %s", raw, MinVersion)
	}
	if v.Core().GreaterThanOrEqual(maxUnsupportedVersion) {
		return fmt.Errorf("OpenTofu %s is not supported (must be below %s)", raw, maxVersionExclusive)
	}
	return nil
}

// tofuVersionPrefix starts the first line of `tofu version` output. The plain
// output is used instead of `version -json` because the JSON payload has no
// field identifying the tool: Terraform emits the same `terraform_version` key,
// so JSON probing would happily accept NIC_TOFU_PATH=$(which terraform) and
// later re-resolve the OpenTofu-registry-pinned lockfiles against
// registry.terraform.io. The banner is unambiguous: "OpenTofu v1.12.5" vs
// "Terraform v1.14.8".
const tofuVersionPrefix = "OpenTofu v"

// queryBinaryVersion runs `<path> version`, verifies the binary identifies
// itself as OpenTofu (not Terraform or something else), and extracts the version.
// It gets its own span because it execs a subprocess — operation-granularity
// work on par with the executor's Init/Plan/Apply wrappers.
func queryBinaryVersion(ctx context.Context, path string) (ver string, err error) {
	ctx, span := otel.Tracer("nebari-infrastructure-core").Start(ctx, "tofu.queryBinaryVersion")
	defer span.End()
	span.SetAttributes(attribute.String("tofu.path", path))
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, binaryVersionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("failed to run %s version: %w: %s", path, err, bytes.TrimSpace(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run %s version: %w", path, err)
	}

	banner, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	banner = strings.TrimSpace(banner)
	switch {
	case strings.HasPrefix(banner, tofuVersionPrefix):
		// Fall through to version extraction below.
	case strings.HasPrefix(banner, "Terraform v"):
		return "", fmt.Errorf("%s is Terraform, not OpenTofu (reported %q); NIC requires OpenTofu because its provider lockfiles pin registry.opentofu.org", path, banner)
	default:
		return "", fmt.Errorf("%s does not identify itself as OpenTofu (`version` reported %q)", path, banner)
	}

	ver = strings.TrimPrefix(banner, tofuVersionPrefix)
	if ver == "" {
		return "", fmt.Errorf("%s version reported no version number (%q)", path, banner)
	}

	return ver, nil
}

// ValidateOverride fails fast when NIC_TOFU_PATH is set but unusable. It is a
// no-op when the override is unset, so providers can call it before creating
// any cloud resources (state buckets, resource groups) without paying a probe
// on the default path. A typo'd override in a packaged or air-gapped
// environment then surfaces before the first cloud API call, not after.
func ValidateOverride(ctx context.Context) error {
	if strings.TrimSpace(os.Getenv(EnvTofuPath)) == "" {
		return nil
	}
	_, err := ResolveExternal(ctx)
	return err
}
