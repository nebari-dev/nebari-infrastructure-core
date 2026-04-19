package aws

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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
