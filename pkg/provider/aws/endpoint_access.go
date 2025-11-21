package aws

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// EndpointAccessConfig represents the EKS endpoint access configuration
type EndpointAccessConfig struct {
	PublicAccess  bool
	PrivateAccess bool
}

// getEndpointAccessConfig determines the endpoint access configuration based on the EKSEndpointAccess setting.
// Valid values: "public", "private", "public-and-private"
// Default: both public and private access enabled (recommended for nodes in private subnets)
func getEndpointAccessConfig(ctx context.Context, endpointAccess string) EndpointAccessConfig {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.getEndpointAccessConfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("endpoint_access_setting", endpointAccess),
	)

	config := EndpointAccessConfig{
		PublicAccess:  DefaultEndpointPublic,
		PrivateAccess: DefaultEndpointPrivate,
	}

	switch endpointAccess {
	case "private":
		config.PublicAccess = false
		config.PrivateAccess = true
	case "public-and-private":
		config.PublicAccess = true
		config.PrivateAccess = true
	case "public":
		// Public only - disable private access
		config.PrivateAccess = false
	case "":
		// Use defaults (public=true, private=true)
	}

	span.SetAttributes(
		attribute.Bool("public_access", config.PublicAccess),
		attribute.Bool("private_access", config.PrivateAccess),
	)

	return config
}
