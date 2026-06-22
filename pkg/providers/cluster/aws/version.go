package aws

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// validateK8sVersionUpgrade validates that a Kubernetes version upgrade is valid.
// EKS requires incremental upgrades - you cannot skip minor versions.
// For example: 1.34 → 1.30 is invalid (must go 1.34 → 1.29 → 1.30)
func validateK8sVersionUpgrade(ctx context.Context, current, desired string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.validateK8sVersionUpgrade")
	defer span.End()

	span.SetAttributes(
		attribute.String("current_version", current),
		attribute.String("desired_version", desired),
	)

	// If versions are the same, no upgrade needed
	if current == desired {
		span.SetAttributes(attribute.Bool("upgrade_needed", false))
		return nil
	}

	// Parse versions
	currentMajor, currentMinor, err := parseK8sVersion(current)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("invalid current Kubernetes version %s: %w", current, err)
	}

	desiredMajor, desiredMinor, err := parseK8sVersion(desired)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("invalid desired Kubernetes version %s: %w", desired, err)
	}

	span.SetAttributes(
		attribute.Int("current_major", currentMajor),
		attribute.Int("current_minor", currentMinor),
		attribute.Int("desired_major", desiredMajor),
		attribute.Int("desired_minor", desiredMinor),
	)

	// Major version must be the same (Kubernetes doesn't support major version changes in-place)
	if currentMajor != desiredMajor {
		err := fmt.Errorf(
			"cannot change Kubernetes major version in-place (current: %s, desired: %s). "+
				"Major version upgrades require cluster recreation",
			current,
			desired,
		)
		span.RecordError(err)
		return err
	}

	// Downgrade not allowed
	if desiredMinor < currentMinor {
		err := fmt.Errorf(
			"cannot downgrade Kubernetes version (current: %s, desired: %s). "+
				"Downgrades are not supported",
			current,
			desired,
		)
		span.RecordError(err)
		return err
	}

	// Check if upgrade skips minor versions (only one minor version upgrade at a time)
	if desiredMinor > currentMinor+1 {
		err := fmt.Errorf(
			"cannot skip Kubernetes minor versions (current: %s, desired: %s). "+
				"EKS requires incremental upgrades. Upgrade to 1.%d first, then to 1.%d",
			current,
			desired,
			currentMinor+1,
			desiredMinor,
		)
		span.RecordError(err)
		return err
	}

	span.SetAttributes(attribute.Bool("upgrade_valid", true))
	return nil
}

// parseK8sVersion parses a Kubernetes version string like "1.34" into major and minor components
func parseK8sVersion(version string) (major, minor int, err error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.parseK8sVersion")
	defer span.End()

	span.SetAttributes(attribute.String("version", version))

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		err = fmt.Errorf("version must be in format 'major.minor' (e.g., '1.34'), got: %s", version)
		span.RecordError(err)
		return 0, 0, err
	}

	major, err = strconv.Atoi(parts[0])
	if err != nil {
		err = fmt.Errorf("invalid major version in %s: %w", version, err)
		span.RecordError(err)
		return 0, 0, err
	}

	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		err = fmt.Errorf("invalid minor version in %s: %w", version, err)
		span.RecordError(err)
		return 0, 0, err
	}

	span.SetAttributes(
		attribute.Int("major", major),
		attribute.Int("minor", minor),
	)

	return major, minor, nil
}
