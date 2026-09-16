package endpoint

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	DefaultNamespace     = "envoy-gateway-system"
	DefaultLabelSelector = "gateway.envoyproxy.io/owning-gateway-name=nebari-gateway"
	DefaultTimeout       = 5 * time.Minute
	DefaultPollInterval  = 5 * time.Second

	// probeDialTimeout and probeTimeout bound ProbeGateway's single attempt.
	// They are short on purpose: callers own retry policy (nic outputs --wait
	// polls), so one attempt must fail fast rather than eat the caller's
	// window on an address that does not answer.
	probeDialTimeout = 5 * time.Second
	probeTimeout     = 10 * time.Second
)

// LoadBalancerEndpoint holds the external endpoint assigned to the load balancer.
type LoadBalancerEndpoint struct {
	Hostname string
	IP       string
	// Port is the HTTPS port the platform is served on. Zero means the
	// standard 443. Non-standard values occur on host-port gateways (local
	// kind clusters with cluster.local.https_port set). Load balancers found
	// by inspecting the cluster always serve on 443 and leave this zero.
	Port int
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

// Check performs a single attempt to find the load balancer endpoint and
// returns an error if it is not yet available. Unlike GetLoadBalancerEndpoint
// it never polls, so callers that own their own retry policy (or want a
// one-shot read) do not inherit the polling timeout. WithTimeout and
// WithPollInterval have no effect here.
func Check(ctx context.Context, client kubernetes.Interface, opts ...Option) (*LoadBalancerEndpoint, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "endpoint.Check")
	defer span.End()

	cfg := defaultOptions()
	for _, opt := range opts {
		opt(cfg)
	}

	span.SetAttributes(
		attribute.String("namespace", cfg.namespace),
		attribute.String("label_selector", cfg.labelSelector),
	)

	ep, err := checkEndpoint(ctx, client, cfg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return ep, nil
}

// checkEndpoint performs a single attempt to find the load balancer endpoint.
func checkEndpoint(ctx context.Context, client kubernetes.Interface, cfg *options) (*LoadBalancerEndpoint, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "endpoint.checkEndpoint")
	defer span.End()

	svc, err := gatewayService(ctx, client, cfg)
	if err != nil {
		return nil, err
	}

	ingress := svc.Status.LoadBalancer.Ingress
	if len(ingress) == 0 {
		return nil, fmt.Errorf("load balancer not ready: no ingress entries")
	}

	return &LoadBalancerEndpoint{
		Hostname: ingress[0].Hostname,
		IP:       ingress[0].IP,
	}, nil
}

// CheckNodePorts verifies the gateway's service carries every NodePort in
// want, in one attempt with no polling. Host-port gateways (local kind
// clusters) have no load balancer status to observe: the cluster-side fact
// their host port mappings depend on is the service's pinned nodePort values.
// A missing service or a different nodePort means the mapped host ports lead
// nowhere (for example when the EnvoyProxy service patch no longer matches
// the generated service and Kubernetes assigned random NodePorts), so the
// caller must not report the host address as reachable.
func CheckNodePorts(ctx context.Context, client kubernetes.Interface, want []int32, opts ...Option) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "endpoint.CheckNodePorts")
	defer span.End()

	cfg := defaultOptions()
	for _, opt := range opts {
		opt(cfg)
	}

	span.SetAttributes(
		attribute.String("namespace", cfg.namespace),
		attribute.String("label_selector", cfg.labelSelector),
	)

	svc, err := gatewayService(ctx, client, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	got := make([]int32, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		got = append(got, p.NodePort)
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			err := fmt.Errorf("gateway service %s/%s does not carry nodePort %d (has %v): the EnvoyProxy service patch has not applied", svc.Namespace, svc.Name, w, got)
			span.RecordError(err)
			return err
		}
	}
	return nil
}

// ProbeGateway sends one HTTPS request for domain through the gateway at
// address, with no polling. address may be an IP or a hostname: the TCP
// connection is pinned to it while SNI and Host stay domain, because the
// gateway routes by hostname. Any HTTP response, whatever its status code,
// proves the path to the gateway. Redirects are not followed. TLS is
// deliberately not verified, since reachability is the claim being checked
// and certificate trust is the caller's concern (local clusters serve
// self-signed certificates).
func ProbeGateway(ctx context.Context, address, domain string, port int) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "endpoint.ProbeGateway")
	defer span.End()

	target := net.JoinHostPort(address, strconv.Itoa(port))
	span.SetAttributes(
		attribute.String("target", target),
		attribute.String("domain", domain),
	)

	probeClient := &http.Client{
		Timeout: probeTimeout,
		Transport: &http.Transport{
			// Dial the gateway address no matter what host the URL names.
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: probeDialTimeout}).DialContext(ctx, network, target)
			},
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // reachability probe, not a trust decision; see the doc comment
				ServerName:         domain,
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("https://%s/", net.JoinHostPort(domain, strconv.Itoa(port)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("build gateway probe request: %w", err)
	}

	resp, err := probeClient.Do(req)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("the gateway did not answer an HTTPS request for %s at %s: %w", domain, target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	span.SetAttributes(attribute.Int("status_code", resp.StatusCode))
	return nil
}

// gatewayService finds the gateway's service by label selector. If multiple
// services match the selector, the first one is used. In practice, Envoy
// Gateway creates exactly one service per Gateway resource.
func gatewayService(ctx context.Context, client kubernetes.Interface, cfg *options) (*corev1.Service, error) {
	services, err := client.CoreV1().Services(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, fmt.Errorf("no services found in namespace %q matching %q", cfg.namespace, cfg.labelSelector)
	}

	return &services.Items[0], nil
}
