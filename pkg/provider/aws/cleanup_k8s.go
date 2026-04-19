package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	gatewayName      = "nebari-gateway"
	gatewayNamespace = "envoy-gateway-system"
)

var gatewayResource = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "gateways",
}

// deleteNebariGateway deletes the Gateway CR reconciled by envoy-gateway. That
// deletion cascades to the managed Service, which fires LBC's finalizer and
// removes the backing NLB. NotFound is treated as success.
func deleteNebariGateway(ctx context.Context, dyn dynamic.Interface) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.deleteNebariGateway")
	defer span.End()
	span.SetAttributes(
		attribute.String("gateway_name", gatewayName),
		attribute.String("gateway_namespace", gatewayNamespace),
	)

	err := dyn.Resource(gatewayResource).Namespace(gatewayNamespace).Delete(ctx, gatewayName, metav1.DeleteOptions{})
	if err == nil {
		span.SetAttributes(attribute.Bool("gateway_deleted", true))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleted Gateway %s/%s", gatewayNamespace, gatewayName)).
			WithResource("gateway").WithAction("deleting"))
		return nil
	}
	if apierrors.IsNotFound(err) {
		span.SetAttributes(attribute.Bool("gateway_deleted", false))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Gateway %s/%s not found (already gone)", gatewayNamespace, gatewayName)).
			WithResource("gateway").WithAction("deleting"))
		return nil
	}
	span.RecordError(err)
	return fmt.Errorf("failed to delete Gateway %s/%s: %w", gatewayNamespace, gatewayName, err)
}
