package endpoint

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultNamespace     = "envoy-gateway-system"
	DefaultLabelSelector = "gateway.envoyproxy.io/owning-gateway-name=nebari-gateway"
	DefaultTimeout       = 5 * time.Minute
	DefaultPollInterval  = 5 * time.Second
)

// LoadBalancerEndpoint holds the external endpoint assigned to the load balancer.
type LoadBalancerEndpoint struct {
	Hostname string
	IP       string
}

// Option configures the behavior of GetLoadBalancerEndpoint.
type Option func(*options)

type options struct {
	namespace     string
	labelSelector string
	timeout       time.Duration
	pollInterval  time.Duration
}

func defaultOptions() *options {
	return &options{
		namespace:     DefaultNamespace,
		labelSelector: DefaultLabelSelector,
		timeout:       DefaultTimeout,
		pollInterval:  DefaultPollInterval,
	}
}

// WithNamespace sets the Kubernetes namespace to search for the service.
func WithNamespace(ns string) Option {
	return func(o *options) { o.namespace = ns }
}

// WithLabelSelector sets the label selector used to find the service.
func WithLabelSelector(sel string) Option {
	return func(o *options) { o.labelSelector = sel }
}

// WithTimeout sets the maximum duration to wait for the endpoint.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithPollInterval sets the interval between polling attempts.
func WithPollInterval(d time.Duration) Option {
	return func(o *options) { o.pollInterval = d }
}

// GetLoadBalancerEndpoint polls the Kubernetes API for a service matching the
// configured label selector and returns the load balancer endpoint once available.
// It keeps polling for both service creation and ingress assignment until the
// timeout expires. This handles the case where ArgoCD hasn't yet reconciled
// the Gateway resource that triggers service creation.
func GetLoadBalancerEndpoint(ctx context.Context, client kubernetes.Interface, opts ...Option) (*LoadBalancerEndpoint, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "endpoint.GetLoadBalancerEndpoint")
	defer span.End()

	cfg := defaultOptions()
	for _, opt := range opts {
		opt(cfg)
	}

	span.SetAttributes(
		attribute.String("namespace", cfg.namespace),
		attribute.String("label_selector", cfg.labelSelector),
		attribute.String("timeout", cfg.timeout.String()),
	)

	deadline := time.After(cfg.timeout)
	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	// Check immediately before entering the polling loop.
	if ep, err := checkEndpoint(ctx, client, cfg); err == nil {
		span.SetAttributes(
			attribute.String("hostname", ep.Hostname),
			attribute.String("ip", ep.IP),
		)
		return ep, nil
	}

	for {
		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			return nil, fmt.Errorf("context cancelled while waiting for load balancer: %w", ctx.Err())
		case <-deadline:
			err := fmt.Errorf("timed out waiting for load balancer endpoint after %s", cfg.timeout)
			span.RecordError(err)
			return nil, err
		case <-ticker.C:
			ep, err := checkEndpoint(ctx, client, cfg)
			if err == nil {
				span.SetAttributes(
					attribute.String("hostname", ep.Hostname),
					attribute.String("ip", ep.IP),
				)
				return ep, nil
			}
		}
	}
}

// checkEndpoint performs a single attempt to find the load balancer endpoint.
// If multiple services match the selector, the first one is used. In practice,
// Envoy Gateway creates exactly one service per Gateway resource.
func checkEndpoint(ctx context.Context, client kubernetes.Interface, cfg *options) (*LoadBalancerEndpoint, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "endpoint.checkEndpoint")
	defer span.End()

	services, err := client.CoreV1().Services(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, fmt.Errorf("no services found in namespace %q matching %q", cfg.namespace, cfg.labelSelector)
	}

	svc := services.Items[0]
	ingress := svc.Status.LoadBalancer.Ingress
	if len(ingress) == 0 {
		return nil, fmt.Errorf("load balancer not ready: no ingress entries")
	}

	return &LoadBalancerEndpoint{
		Hostname: ingress[0].Hostname,
		IP:       ingress[0].IP,
	}, nil
}
