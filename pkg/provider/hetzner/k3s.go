package hetzner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// resolveK3sVersion resolves a Kubernetes version (e.g., "1.32" or "1.32.0")
// to a full k3s release tag (e.g., "v1.32.12+k3s1") by querying the hetzner-k3s
// binary for its supported releases. This ensures the resolved version is actually
// supported by hetzner-k3s, not just published on GitHub.
//
// If version already contains "+k3s", it's returned as-is.
// binaryPath is the path to the hetzner-k3s binary; pass "" to use the releasesFunc
// override (for testing).
func resolveK3sVersion(ctx context.Context, version string, binaryPath string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.resolveK3sVersion")
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

	releases, err := getHetznerK3sReleases(ctx, binaryPath)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	// Releases are returned newest-first; find the first stable match.
	for _, tag := range releases {
		if strings.Contains(tag, "-rc") {
			continue
		}
		if strings.HasPrefix(tag, matchPrefix) {
			span.SetAttributes(attribute.String("resolved_version", tag))
			return tag, nil
		}
	}

	err = fmt.Errorf("no supported k3s release found for kubernetes version %q (run 'hetzner-k3s releases' to see available versions)", version)
	span.RecordError(err)
	return "", err
}

// getHetznerK3sReleases runs `hetzner-k3s releases` and returns the list of
// supported version tags. Results are returned in the order the binary outputs
// them (newest first).
func getHetznerK3sReleases(ctx context.Context, binaryPath string) ([]string, error) {
	cmd := exec.CommandContext(ctx, binaryPath, "releases")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get hetzner-k3s releases: %w: %s", err, stderr.String())
	}

	var releases []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		tag := strings.TrimSpace(line)
		if tag != "" {
			releases = append(releases, tag)
		}
	}
	return releases, nil
}
