package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Validate checks that cfg is well-formed and references providers that are
// actually registered. It performs no I/O against cloud APIs. Returns nil
// when cfg is valid, or an error describing the first validation failure.
func (c *Client) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Validate")
	defer span.End()

	if err := cfg.Validate(validateOptions(ctx, c.registry)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	return nil
}
