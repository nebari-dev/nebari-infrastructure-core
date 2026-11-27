package config

import (
	"context"
	"fmt"
	"math"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/api/resource"
)

// DefaultGatewayRequestBufferLimit is the request buffer limit rendered when
// gateway.request_buffer_limit is unset. It is far above realistic LLM client
// payloads (#389 reports coding-assistant requests of tens to hundreds of KB)
// while keeping the worst case well inside the Envoy proxy pod's 512Mi memory
// limit (manifests/networking/envoyproxy.yaml): the limit applies per
// connection.
const DefaultGatewayRequestBufferLimit = "10Mi"

// MinGatewayRequestBufferLimitBytes is the smallest accepted limit: Envoy
// Gateway's own default of 32Ki (internal/xds/translator/listener.go). A
// smaller value only makes the gateway reject more requests, and is most
// likely a missing unit (50 meant as 50Mi).
const MinGatewayRequestBufferLimitBytes = 32 * 1024

// gatewayBufferLimitPattern is the pattern Envoy Gateway's ClientTrafficPolicy
// CRD enforces on connection.bufferLimit (api/v1alpha1/connection_types.go,
// v1.9.1). Checking it here turns a value the API server would reject at sync
// time, leaving the gateway-config Application failing, into a validate error.
var gatewayBufferLimitPattern = regexp.MustCompile(`^[1-9]+[0-9]*([EPTGMK]i|[EPTGMk])?$`)

// GatewayConfig tunes the shared nebari-gateway that NIC deploys.
//
// request_buffer_limit bounds the size of a request the gateway will buffer
// per client connection. Envoy AI Gateway buffers the whole request body to
// read the model name, so a larger request is rejected with HTTP 413 before it
// reaches the model backend. Envoy's own default of 32Ki is too small for
// coding assistants, whose requests carry file context and conversation
// history. The limit is an upper bound, not a preallocation, but every
// connection can grow to it, so a large value raises worst-case gateway memory:
// the Envoy proxy pod has a 512Mi memory limit, and a handful of slow
// connections each holding a large limit can get it OOM-killed. Raise the
// limit only as far as your largest requests need.
//
// Example YAML:
//
//	gateway:
//	  request_buffer_limit: 20Mi
type GatewayConfig struct {
	// Largest request buffered per client connection; larger ones get HTTP 413.
	// Default: 10Mi. A whole-number quantity (e.g. 20Mi, 100M, 1048576), from 32Ki (32768 bytes) to 4294967295 bytes.
	RequestBufferLimit string `yaml:"request_buffer_limit,omitempty"`
}

// RequestBufferLimitOrDefault returns the configured request buffer limit, or
// DefaultGatewayRequestBufferLimit when the block or the field is unset.
func (g *GatewayConfig) RequestBufferLimitOrDefault(ctx context.Context) string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "config.GatewayConfig.RequestBufferLimitOrDefault")
	defer span.End()

	limit := DefaultGatewayRequestBufferLimit
	if g != nil && g.RequestBufferLimit != "" {
		limit = g.RequestBufferLimit
	}
	span.SetAttributes(
		attribute.String("gateway.request_buffer_limit", limit),
		attribute.Bool("gateway.request_buffer_limit.default", limit == DefaultGatewayRequestBufferLimit),
	)
	return limit
}

// Validate checks the gateway block. nil-safe: a nil GatewayConfig, or an
// unset limit, is valid and uses the default.
func (g *GatewayConfig) Validate(ctx context.Context) (err error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "config.GatewayConfig.Validate")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	if g == nil || g.RequestBufferLimit == "" {
		return nil
	}
	limit := g.RequestBufferLimit
	span.SetAttributes(attribute.String("gateway.request_buffer_limit", limit))
	if !gatewayBufferLimitPattern.MatchString(limit) {
		return fmt.Errorf("request_buffer_limit %q must be a positive whole-number quantity such as 20Mi, 100M or 1048576", limit)
	}
	// Envoy Gateway stores the limit as a uint32 and rejects anything larger
	// (internal/gatewayapi/clienttrafficpolicy.go, v1.9.1).
	q, err := resource.ParseQuantity(limit)
	if err != nil {
		return fmt.Errorf("request_buffer_limit %q: %w", limit, err)
	}
	bytes, ok := q.AsInt64()
	if !ok || bytes > math.MaxUint32 {
		return fmt.Errorf("request_buffer_limit %q exceeds the maximum of %d bytes", limit, uint64(math.MaxUint32))
	}
	if bytes < MinGatewayRequestBufferLimitBytes {
		return fmt.Errorf("request_buffer_limit %q is below the minimum of %d bytes (32Ki, Envoy's default); did you omit a unit such as Mi?", limit, MinGatewayRequestBufferLimitBytes)
	}
	return nil
}
