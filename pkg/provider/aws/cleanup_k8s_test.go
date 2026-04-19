package aws

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func gatewayGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
}

func newGatewayUnstructured() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway"})
	u.SetNamespace(gatewayNamespace)
	u.SetName(gatewayName)
	return u
}

func newDynamicClientWithGateway(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		gatewayGVR().GroupVersion().WithKind("GatewayList"),
		&unstructured.UnstructuredList{},
	)
	listKinds := map[schema.GroupVersionResource]string{gatewayGVR(): "GatewayList"}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, objs...)
}

func TestDeleteNebariGateway(t *testing.T) {
	tests := []struct {
		name         string
		seedObjects  []runtime.Object
		wantErr      bool
		wantNotFound bool
	}{
		{
			name:        "gateway exists - deleted",
			seedObjects: []runtime.Object{newGatewayUnstructured()},
			wantErr:     false,
		},
		{
			name:         "gateway absent - no error",
			seedObjects:  nil,
			wantErr:      false,
			wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newDynamicClientWithGateway(tt.seedObjects...)
			err := deleteNebariGateway(context.Background(), client)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			_, err = client.Resource(gatewayGVR()).Namespace(gatewayNamespace).Get(context.Background(), gatewayName, metav1.GetOptions{})
			if tt.wantNotFound {
				return
			}
			if err == nil {
				t.Fatalf("expected Gateway to be deleted, but Get succeeded")
			}
		})
	}
}

func TestSweepLoadBalancerServices(t *testing.T) {
	svcLB := func(ns, name string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		}
	}
	svcClusterIP := func(ns, name string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		}
	}

	tests := []struct {
		name           string
		seed           []runtime.Object
		wantDeletedKey []string
	}{
		{
			name: "deletes only LoadBalancer services across namespaces",
			seed: []runtime.Object{
				svcLB("default", "a"),
				svcLB("envoy-gateway-system", "envoy-service"),
				svcClusterIP("default", "cip"),
			},
			wantDeletedKey: []string{"default/a", "envoy-gateway-system/envoy-service"},
		},
		{
			name:           "no LB services - no deletes",
			seed:           []runtime.Object{svcClusterIP("default", "only-cip")},
			wantDeletedKey: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//nolint:staticcheck // fake.NewSimpleClientset is still the standard
			c := k8sfake.NewSimpleClientset(tt.seed...)

			if err := sweepLoadBalancerServices(context.Background(), c); err != nil {
				t.Fatalf("sweepLoadBalancerServices returned %v", err)
			}

			list, err := c.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("post-sweep list failed: %v", err)
			}
			remaining := map[string]corev1.ServiceType{}
			for _, s := range list.Items {
				remaining[s.Namespace+"/"+s.Name] = s.Spec.Type
			}
			for _, key := range tt.wantDeletedKey {
				if _, still := remaining[key]; still {
					t.Errorf("expected %s to be deleted but it's still present", key)
				}
			}
			for key, typ := range remaining {
				if typ == corev1.ServiceTypeLoadBalancer {
					t.Errorf("LoadBalancer service %s should have been deleted", key)
				}
			}
		})
	}
}

func TestWaitForLoadBalancerServicesGone(t *testing.T) {
	t.Run("returns nil when no LB services remain", func(t *testing.T) {
		//nolint:staticcheck
		c := k8sfake.NewSimpleClientset()
		err := waitForLoadBalancerServicesGone(context.Background(), c, 500*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns error on timeout when LB services persist", func(t *testing.T) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "stuck"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		}
		//nolint:staticcheck
		c := k8sfake.NewSimpleClientset(svc)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		err := waitForLoadBalancerServicesGone(ctx, c, 10*time.Second)
		if err == nil {
			t.Fatalf("expected timeout error, got nil")
		}
	})
}
