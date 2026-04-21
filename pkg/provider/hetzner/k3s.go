package hetzner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// k3sVersionPattern matches valid k3s release tags.
// Examples: v1.32.12+k3s1, v1.32.12-rc1+k3s1, v1.32.12-alpha1+k3s1
var k3sVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-(?:rc|alpha|beta)\d*)?\+k3s\d+$`)

// resolveK3sVersion resolves a Kubernetes version (e.g., "1.32" or "1.32.0")
// to a full k3s release tag (e.g., "v1.32.12+k3s1") by matching against the
// provided list of available releases. This is a pure function - the caller is
// responsible for fetching the releases list from the hetzner-k3s binary.
//
// If version already contains "+k3s", it's returned as-is.
// releases are expected in oldest-first order (as output by `hetzner-k3s releases`);
// the function iterates in reverse to find the newest matching stable release.
func resolveK3sVersion(ctx context.Context, version string, releases []string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "hetzner.resolveK3sVersion")
	defer span.End()

	span.SetAttributes(attribute.String("requested_version", version))

	if strings.Contains(version, "+k3s") {
		span.SetAttributes(attribute.String("resolved_version", version))
		return version, nil
	}

	normalized := strings.TrimPrefix(version, "v")
	parts := strings.Split(normalized, ".")

	var matchPrefix string
	switch len(parts) {
	case 2:
		matchPrefix = fmt.Sprintf("v%s.%s.", parts[0], parts[1])
	case 3:
		matchPrefix = fmt.Sprintf("v%s.%s.%s+", parts[0], parts[1], parts[2])
	default:
		return "", fmt.Errorf("invalid kubernetes version format: %q (expected MAJOR.MINOR or MAJOR.MINOR.PATCH)", version)
	}

	// Releases from hetzner-k3s are oldest-first; iterate in reverse to find
	// the newest stable match. Skip pre-release versions (rc, alpha, beta).
	for i := len(releases) - 1; i >= 0; i-- {
		tag := releases[i]
		if isPrerelease(tag) {
			continue
		}
		if strings.HasPrefix(tag, matchPrefix) {
			span.SetAttributes(attribute.String("resolved_version", tag))
			return tag, nil
		}
	}

	// Build list of available minor versions for a more helpful error message.
	availableMinors := extractAvailableMinors(releases)
	if len(availableMinors) > 0 {
		return "", fmt.Errorf("no supported k3s release found for kubernetes version %q (available minor versions: %s)",
			version, strings.Join(availableMinors, ", "))
	}
	return "", fmt.Errorf("no supported k3s release found for kubernetes version %q (run 'hetzner-k3s releases' to see available versions)", version)
}

// prereleasePattern matches the pre-release portion of a k3s version tag.
// It's stricter than Contains("-rc") - only matches -rc/-alpha/-beta between
// the patch version and +k3s suffix, e.g., "v1.32.12-rc1+k3s1".
var prereleasePattern = regexp.MustCompile(`-(?:rc|alpha|beta)\d*\+k3s`)

// isPrerelease returns true if the version tag indicates a pre-release version.
// Uses a strict regex pattern to match -rc/-alpha/-beta between patch and +k3s suffix.
func isPrerelease(tag string) bool {
	return prereleasePattern.MatchString(tag)
}

// extractAvailableMinors extracts unique minor versions from release tags.
// Returns a sorted slice like ["1.31", "1.32", "1.33"].
func extractAvailableMinors(releases []string) []string {
	seen := make(map[string]bool)
	var minors []string

	for _, tag := range releases {
		if isPrerelease(tag) {
			continue
		}
		// Extract minor version from tags like "v1.32.12+k3s1"
		tag = strings.TrimPrefix(tag, "v")
		parts := strings.Split(tag, ".")
		if len(parts) >= 2 {
			minor := parts[0] + "." + parts[1]
			if !seen[minor] {
				seen[minor] = true
				minors = append(minors, minor)
			}
		}
	}
	return minors
}

// getHetznerK3sReleases runs `hetzner-k3s releases` and returns the list of
// supported version tags. Results are returned in the order the binary outputs
// them (oldest-first). A 90-second timeout is applied - the binary paginates
// through all k3s tags on a cache miss (~70 pages), which can be slow.
//
// Note: This function has a side effect - hetzner-k3s creates/updates a cache
// file at ~/.hetzner-k3s/k3s-releases.yaml (7-day TTL). The cache path is
// controlled by the binary, not by this code.
func getHetznerK3sReleases(ctx context.Context, binaryPath string) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.getHetznerK3sReleases")
	defer span.End()

	span.SetAttributes(attribute.String("binary.path", binaryPath))

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "releases")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get hetzner-k3s releases: %w: %q", err, stderrStr)
	}

	var releases []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		tag := strings.TrimSpace(line)
		// Validate against version pattern to filter out banner lines,
		// headers, and any other non-version output from the binary.
		if k3sVersionPattern.MatchString(tag) {
			releases = append(releases, tag)
		}
	}

	span.SetAttributes(attribute.Int("release.count", len(releases)))

	if len(releases) == 0 {
		err := fmt.Errorf("hetzner-k3s returned no valid release tags (output format may have changed)")
		span.RecordError(err)
		return nil, err
	}

	return releases, nil
}
