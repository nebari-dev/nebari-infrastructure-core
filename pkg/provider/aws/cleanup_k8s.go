package aws

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

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

// sweepLoadBalancerServices deletes every Service whose spec.type is
// LoadBalancer across every namespace. This covers Envoy Gateway's own
// managed Service (left behind if the Gateway delete raced the operator) and
// any other user-created LoadBalancer Services.
func sweepLoadBalancerServices(ctx context.Context, client kubernetes.Interface) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.sweepLoadBalancerServices")
	defer span.End()

	list, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list services: %w", err)
	}

	var deleted int
	for _, svc := range list.Items {
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		if err := client.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Failed to delete LoadBalancer service %s/%s: %v", svc.Namespace, svc.Name, err)).
				WithResource("service").WithAction("deleting"))
			span.RecordError(err)
			continue
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleted LoadBalancer service %s/%s", svc.Namespace, svc.Name)).
			WithResource("service").WithAction("deleting"))
		deleted++
	}

	span.SetAttributes(attribute.Int("load_balancer_services_deleted", deleted))
	return nil
}

// cleanupKubernetesResources is the Stage 1 happy-path entry point. It builds
// Kubernetes clients from a kubeconfig blob, then delegates to
// runCleanupKubernetesResources. All failures inside are best-effort; callers
// should log-and-continue so the Stage 2 SDK sweep still runs.
//
//nolint:unused // wired into provider.go Destroy in the next task
func cleanupKubernetesResources(ctx context.Context, kubeconfig []byte, clusterName string, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.cleanupKubernetesResources")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("timeout", timeout.String()),
	)

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("cleanup_mode", "unreachable"))
		return fmt.Errorf("parse kubeconfig: %w", err)
	}
	k8s, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("cleanup_mode", "unreachable"))
		return fmt.Errorf("build kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("cleanup_mode", "unreachable"))
		return fmt.Errorf("build dynamic client: %w", err)
	}

	return runCleanupKubernetesResources(ctx, dyn, k8s, timeout)
}

// runCleanupKubernetesResources runs the three-step Stage 1 sequence against
// pre-built clients. Exposed for tests.
func runCleanupKubernetesResources(ctx context.Context, dyn dynamic.Interface, k8s kubernetes.Interface, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.runCleanupKubernetesResources")
	defer span.End()

	if err := deleteNebariGateway(ctx, dyn); err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Gateway delete failed, continuing: %v", err)).
			WithResource("gateway").WithAction("cleanup"))
	}
	if err := sweepLoadBalancerServices(ctx, k8s); err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("LoadBalancer service sweep incomplete: %v", err)).
			WithResource("service").WithAction("cleanup"))
	}
	if err := waitForLoadBalancerServicesGone(ctx, k8s, timeout); err != nil {
		span.SetAttributes(attribute.String("cleanup_mode", "timeout"))
		return err
	}
	span.SetAttributes(attribute.String("cleanup_mode", "graceful"))
	return nil
}

// waitForLoadBalancerServicesGone polls until there are zero Services of
// type=LoadBalancer, or timeout elapses. Timeout is non-fatal in callers; the
// SDK sweep stage will clean up stragglers.
func waitForLoadBalancerServicesGone(ctx context.Context, client kubernetes.Interface, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.waitForLoadBalancerServicesGone")
	defer span.End()
	span.SetAttributes(attribute.String("timeout", timeout.String()))

	deadline := time.Now().Add(timeout)
	const pollInterval = 5 * time.Second

	for {
		list, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to list services while polling: %w", err)
		}

		var remaining int
		for _, svc := range list.Items {
			if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
				remaining++
			}
		}
		if remaining == 0 {
			span.SetAttributes(attribute.Int("load_balancer_services_stragglers", 0))
			return nil
		}

		if time.Now().After(deadline) {
			span.SetAttributes(attribute.Int("load_balancer_services_stragglers", remaining))
			return fmt.Errorf("%d LoadBalancer service(s) still present after %s", remaining, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
