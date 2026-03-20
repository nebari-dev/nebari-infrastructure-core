package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCleanupVolumes_DeletesUnattachedCSIVolumes(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	volumes := volumeListResponse{
		Volumes: []hetznerVolume{
			{ID: 1, Name: "vol-1", Labels: map[string]string{"managed-by": "csi-driver"}, Server: nil},
			{ID: 2, Name: "vol-2", Labels: map[string]string{"managed-by": "csi-driver"}, Server: nil},
		},
	}

	deletedIDs := map[int]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/volumes" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(volumes)
		case r.Method == http.MethodDelete:
			deletedIDs[parseVolumeID(r.URL.Path)] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupVolumes(context.Background())
	if err != nil {
		t.Fatalf("cleanupVolumes() error = %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if !deletedIDs[1] || !deletedIDs[2] {
		t.Errorf("expected volumes 1 and 2 to be deleted, got %v", deletedIDs)
	}
}

func TestCleanupVolumes_SkipsPersistVolumes(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	volumes := volumeListResponse{
		Volumes: []hetznerVolume{
			{ID: 1, Name: "vol-1", Labels: map[string]string{"managed-by": "csi-driver", "persist": "true"}, Server: nil},
			{ID: 2, Name: "vol-2", Labels: map[string]string{"managed-by": "csi-driver"}, Server: nil},
		},
	}

	deletedIDs := map[int]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/volumes" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(volumes)
		case r.Method == http.MethodDelete:
			deletedIDs[parseVolumeID(r.URL.Path)] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupVolumes(context.Background())
	if err != nil {
		t.Fatalf("cleanupVolumes() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if deletedIDs[1] {
		t.Error("volume 1 with persist=true should not be deleted")
	}
	if !deletedIDs[2] {
		t.Error("volume 2 without persist should be deleted")
	}
}

func TestCleanupVolumes_SkipsRunningServerVolumes(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	serverID := 999
	volumes := volumeListResponse{
		Volumes: []hetznerVolume{
			{ID: 1, Name: "vol-1", Labels: map[string]string{"managed-by": "csi-driver"}, Server: &serverID},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/volumes" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(volumes)
		case r.URL.Path == "/servers/999" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(serverResponse{Server: &hetznerServer{ID: 999, Status: "running"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupVolumes(context.Background())
	if err != nil {
		t.Fatalf("cleanupVolumes() error = %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (volume attached to running server)", deleted)
	}
}

func TestLabelPersistVolumes(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	volumes := volumeListResponse{
		Volumes: []hetznerVolume{
			{ID: 1, Name: "vol-1", Labels: map[string]string{"managed-by": "csi-driver"}},
			{ID: 2, Name: "vol-2", Labels: map[string]string{"managed-by": "csi-driver", "persist": "true"}},
		},
	}

	labeledIDs := map[int]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/volumes" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(volumes)
		case r.Method == http.MethodPut:
			labeledIDs[parseVolumeID(r.URL.Path)] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{"id": parseVolumeID(r.URL.Path)}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	err := labelPersistVolumes(context.Background())
	if err != nil {
		t.Fatalf("labelPersistVolumes() error = %v", err)
	}
	if !labeledIDs[1] {
		t.Error("volume 1 should have been labeled")
	}
	if labeledIDs[2] {
		t.Error("volume 2 already had persist=true, should not be re-labeled")
	}
}

func TestCleanupVolumes_NoVolumes(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(volumeListResponse{Volumes: []hetznerVolume{}})
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupVolumes(context.Background())
	if err != nil {
		t.Fatalf("cleanupVolumes() error = %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

// parseVolumeID extracts the volume ID from a URL path like /volumes/123.
func parseVolumeID(path string) int {
	var id int
	_, _ = fmt.Sscanf(path, "/volumes/%d", &id)
	return id
}

// parseLBID extracts the load balancer ID from a URL path like /load_balancers/123.
func parseLBID(path string) int {
	var id int
	_, _ = fmt.Sscanf(path, "/load_balancers/%d", &id)
	return id
}

func TestCleanupLoadBalancers_DeletesOrphaned(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	lbs := loadBalancerListResponse{
		LoadBalancers: []hetznerLoadBalancer{
			{ID: 10, Name: "lb-orphan-1", Labels: map[string]string{"hcloud-ccm/service-uid": "abc"}, Targets: []any{}},
			{ID: 11, Name: "lb-orphan-2", Labels: map[string]string{"hcloud-ccm/service-uid": "def"}, Targets: []any{}},
		},
	}

	deletedIDs := map[int]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/load_balancers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(lbs)
		case r.Method == http.MethodDelete:
			deletedIDs[parseLBID(r.URL.Path)] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupLoadBalancers(context.Background())
	if err != nil {
		t.Fatalf("cleanupLoadBalancers() error = %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if !deletedIDs[10] || !deletedIDs[11] {
		t.Errorf("expected LBs 10 and 11 to be deleted, got %v", deletedIDs)
	}
}

func TestCleanupLoadBalancers_SkipsWithTargets(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	lbs := loadBalancerListResponse{
		LoadBalancers: []hetznerLoadBalancer{
			{ID: 10, Name: "lb-active", Labels: map[string]string{"hcloud-ccm/service-uid": "abc"}, Targets: []any{"server-1"}},
			{ID: 11, Name: "lb-orphan", Labels: map[string]string{"hcloud-ccm/service-uid": "def"}, Targets: []any{}},
		},
	}

	deletedIDs := map[int]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/load_balancers" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(lbs)
		case r.Method == http.MethodDelete:
			deletedIDs[parseLBID(r.URL.Path)] = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupLoadBalancers(context.Background())
	if err != nil {
		t.Fatalf("cleanupLoadBalancers() error = %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if deletedIDs[10] {
		t.Error("LB 10 with targets should not be deleted")
	}
	if !deletedIDs[11] {
		t.Error("LB 11 without targets should be deleted")
	}
}

func TestCleanupLoadBalancers_NoLoadBalancers(t *testing.T) {
	t.Setenv("HETZNER_TOKEN", "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(loadBalancerListResponse{LoadBalancers: []hetznerLoadBalancer{}})
	}))
	defer server.Close()

	origBase := hetznerAPIBase
	setHetznerAPIBase(server.URL)
	defer setHetznerAPIBase(origBase)

	deleted, err := cleanupLoadBalancers(context.Background())
	if err != nil {
		t.Fatalf("cleanupLoadBalancers() error = %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}
