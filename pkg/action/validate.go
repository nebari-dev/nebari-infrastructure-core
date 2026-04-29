package action

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Validate checks that a NebariConfig is well-formed and references providers
// that are actually registered. It performs no I/O against cloud APIs.
type Validate struct{}

// Run returns nil when cfg is valid, or an error describing the first
// validation failure. It does not mutate cfg.
func (v *Validate) Run(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "action.Validate")
	defer span.End()

	reg, err := defaultRegistry(ctx)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("build default registry: %w", err)
	}

	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	return nil
}
