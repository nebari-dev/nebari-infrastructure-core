package local

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func markerData(t *testing.T, client *fake.Clientset) map[string]string {
	t.Helper()
	marker, err := client.CoreV1().ConfigMaps(markerNamespace).Get(context.Background(), markerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return marker.Data
}

func TestRecordClusterPorts(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	if err := recordClusterPorts(ctx, client, 80, 443); err != nil {
		t.Fatalf("recordClusterPorts() error: %v", err)
	}
	data := markerData(t, client)
	if data[markerHTTPKey] != "80" || data[markerHTTPSKey] != "443" {
		t.Errorf("marker data = %v, want http_port 80 and https_port 443", data)
	}

	// Recording again overwrites rather than failing on the existing marker.
	if err := recordClusterPorts(ctx, client, 8080, 8443); err != nil {
		t.Fatalf("recordClusterPorts() on existing marker error: %v", err)
	}
	data = markerData(t, client)
	if data[markerHTTPKey] != "8080" || data[markerHTTPSKey] != "8443" {
		t.Errorf("marker data after rewrite = %v, want http_port 8080 and https_port 8443", data)
	}
}

func TestVerifyClusterPorts(t *testing.T) {
	ctx := context.Background()

	t.Run("matching ports pass", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := recordClusterPorts(ctx, client, 80, 443); err != nil {
			t.Fatalf("recordClusterPorts() error: %v", err)
		}
		if err := verifyClusterPorts(ctx, client, "test-project", 80, 443); err != nil {
			t.Errorf("verifyClusterPorts() unexpected error: %v", err)
		}
	})

	t.Run("changed https_port fails naming both values", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := recordClusterPorts(ctx, client, 80, 443); err != nil {
			t.Fatalf("recordClusterPorts() error: %v", err)
		}
		err := verifyClusterPorts(ctx, client, "test-project", 80, 8443)
		if err == nil {
			t.Fatal("verifyClusterPorts() expected error, got nil")
		}
		for _, want := range []string{"https_port 443", "8443", "recreate", "test-project"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should contain %q", err.Error(), want)
			}
		}
	})

	t.Run("changed http_port fails", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := recordClusterPorts(ctx, client, 80, 443); err != nil {
			t.Fatalf("recordClusterPorts() error: %v", err)
		}
		if err := verifyClusterPorts(ctx, client, "test-project", 8080, 443); err == nil {
			t.Fatal("verifyClusterPorts() expected error, got nil")
		}
	})

	t.Run("missing marker adopts the configured ports", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := verifyClusterPorts(ctx, client, "test-project", 80, 8443); err != nil {
			t.Fatalf("verifyClusterPorts() unexpected error: %v", err)
		}
		data := markerData(t, client)
		if data[markerHTTPKey] != "80" || data[markerHTTPSKey] != "8443" {
			t.Errorf("adopted marker data = %v, want http_port 80 and https_port 8443", data)
		}
		// The adopted values hold on the next verification.
		if err := verifyClusterPorts(ctx, client, "test-project", 80, 8443); err != nil {
			t.Errorf("verifyClusterPorts() after adoption unexpected error: %v", err)
		}
	})

	t.Run("marker missing a key is rewritten, not compared", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		if err := recordClusterPorts(ctx, client, 80, 443); err != nil {
			t.Fatalf("recordClusterPorts() error: %v", err)
		}
		marker, err := client.CoreV1().ConfigMaps(markerNamespace).Get(ctx, markerName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("read marker: %v", err)
		}
		delete(marker.Data, markerHTTPSKey)
		if _, err := client.CoreV1().ConfigMaps(markerNamespace).Update(ctx, marker, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("update marker: %v", err)
		}

		if err := verifyClusterPorts(ctx, client, "test-project", 80, 8443); err != nil {
			t.Fatalf("verifyClusterPorts() unexpected error: %v", err)
		}
		if data := markerData(t, client); data[markerHTTPSKey] != "8443" {
			t.Errorf("rewritten marker data = %v, want https_port 8443", data)
		}
	})
}
