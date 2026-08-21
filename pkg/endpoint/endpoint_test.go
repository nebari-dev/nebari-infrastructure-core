package endpoint

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetLoadBalancerEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		objects     []runtime.Object
		opts        []Option
		wantHost    string
		wantIP      string
		errContains string
	}{
		{
			name: "returns hostname from service with LB ingress",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "envoy-gateway-svc",
						Namespace: DefaultNamespace,
						Labels: map[string]string{
							"gateway.envoyproxy.io/owning-gateway-name": "nebari-gateway",
						},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{Hostname: "abc123.us-west-2.elb.amazonaws.com"},
							},
						},
					},
				},
			},
			wantHost: "abc123.us-west-2.elb.amazonaws.com",
		},
		{
			name: "returns IP from service with LB ingress",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "envoy-gateway-svc",
						Namespace: DefaultNamespace,
						Labels: map[string]string{
							"gateway.envoyproxy.io/owning-gateway-name": "nebari-gateway",
						},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{IP: "34.120.50.99"},
							},
						},
					},
				},
			},
			wantIP: "34.120.50.99",
		},
		{
			name:    "polls until timeout when no matching services found",
			objects: []runtime.Object{},
			opts: []Option{
				WithTimeout(100 * time.Millisecond),
				WithPollInterval(10 * time.Millisecond),
			},
			errContains: "timed out",
		},
		{
			name: "returns error when LB ingress is empty after timeout",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "envoy-gateway-svc",
						Namespace: DefaultNamespace,
						Labels: map[string]string{
							"gateway.envoyproxy.io/owning-gateway-name": "nebari-gateway",
						},
					},
					Status: corev1.ServiceStatus{},
				},
			},
			opts: []Option{
				WithTimeout(100 * time.Millisecond),
				WithPollInterval(10 * time.Millisecond),
			},
			errContains: "timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			ep, err := GetLoadBalancerEndpoint(context.Background(), client, tt.opts...)

			if tt.errContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ep.Hostname != tt.wantHost {
				t.Errorf("hostname = %q, want %q", ep.Hostname, tt.wantHost)
			}
			if ep.IP != tt.wantIP {
				t.Errorf("ip = %q, want %q", ep.IP, tt.wantIP)
			}
		})
	}
}

func TestGetLoadBalancerEndpoint_ContextCancelled(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envoy-gateway-svc",
			Namespace: DefaultNamespace,
			Labels: map[string]string{
				"gateway.envoyproxy.io/owning-gateway-name": "nebari-gateway",
			},
		},
		Status: corev1.ServiceStatus{},
	}
	client := fake.NewSimpleClientset(svc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GetLoadBalancerEndpoint(ctx, client,
		WithTimeout(5*time.Second),
		WithPollInterval(10*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected error when context is cancelled, got nil")
	}
	if !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("expected error containing %q, got %q", "context cancelled", err.Error())
	}
}

// gatewaySvc builds a LoadBalancer service carrying the default owning-gateway
// label, so tests exercise the same selector production code uses.
func gatewaySvc(ingress ...corev1.LoadBalancerIngress) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envoy-gateway-svc",
			Namespace: DefaultNamespace,
			Labels: map[string]string{
				"gateway.envoyproxy.io/owning-gateway-name": "nebari-gateway",
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: ingress},
		},
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name        string
		objects     []runtime.Object
		wantHost    string
		wantIP      string
		errContains string
	}{
		{
			name:    "returns endpoint without waiting when ingress is assigned",
			objects: []runtime.Object{gatewaySvc(corev1.LoadBalancerIngress{IP: "10.89.0.2"})},
			wantIP:  "10.89.0.2",
		},
		{
			name:        "errors immediately when the service has no ingress",
			objects:     []runtime.Object{gatewaySvc()},
			errContains: "load balancer not ready",
		},
		{
			name:        "errors immediately when no service matches the selector",
			objects:     nil,
			errContains: "no services found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)

			// A deadline Check must not consume: a polling implementation would
			// blow the test's own timeout instead of returning at once.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			ep, err := Check(ctx, client)

			if tt.errContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ep.IP != tt.wantIP {
				t.Errorf("ip = %q, want %q", ep.IP, tt.wantIP)
			}
			if ep.Hostname != tt.wantHost {
				t.Errorf("hostname = %q, want %q", ep.Hostname, tt.wantHost)
			}
		})
	}
}

// TestExportedFunctionsStartSpans pins the project's instrumentation
// convention: an exported function in pkg/ that accepts a context opens a span
// of its own. Delegating to an already-instrumented helper is not enough - the
// caller's own frame is what makes a trace attributable to the operation the
// operator asked for.
func TestExportedFunctionsStartSpans(t *testing.T) {
	client := fake.NewSimpleClientset(gatewaySvc(corev1.LoadBalancerIngress{IP: "10.89.0.2"}))

	tests := []struct {
		name     string
		call     func(context.Context) error
		wantSpan string
	}{
		{
			name:     "Check",
			call:     func(ctx context.Context) error { _, err := Check(ctx, client); return err },
			wantSpan: "endpoint.Check",
		},
		{
			name: "GetLoadBalancerEndpoint",
			call: func(ctx context.Context) error {
				_, err := GetLoadBalancerEndpoint(ctx, client)
				return err
			},
			wantSpan: "endpoint.GetLoadBalancerEndpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			defer otel.SetTracerProvider(previous)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if err := tt.call(ctx); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := provider.ForceFlush(context.Background()); err != nil {
				t.Fatalf("flush spans: %v", err)
			}

			var names []string
			for _, span := range recorder.Ended() {
				names = append(names, span.Name())
			}

			found := false
			for _, name := range names {
				if name == tt.wantSpan {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no span named %q was recorded; got %v", tt.wantSpan, names)
			}
		})
	}
}
