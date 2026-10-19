package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
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

// actionReply is one response to GET /actions/42: an HTTP code and, for 200,
// the action status.
type actionReply struct {
	code   int
	status string
}

func TestCleanupLoadBalancers_DeleteResponses(t *testing.T) {
	const deleteAccepted = `{"action":{"id":42,"status":"running"}}`

	tests := []struct {
		name         string
		deleteStatus int
		deleteBody   string
		// actionReplies answer successive GET /actions/42 calls; the last repeats.
		actionReplies []actionReply
		actionError   string
		pollInterval  time.Duration
		timeout       time.Duration
		// cancelParent cancels the caller's context shortly after the first action GET.
		cancelParent   bool
		wantDeleted    int
		wantErr        string
		wantActionGets int
	}{
		{name: "204 empty body", deleteStatus: http.StatusNoContent, wantDeleted: 1},
		{name: "404 already gone", deleteStatus: http.StatusNotFound, wantDeleted: 1},
		{
			name:           "200 with action that succeeds",
			deleteStatus:   http.StatusOK,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{200, "running"}, {200, "success"}},
			wantDeleted:    1,
			wantActionGets: 2,
		},
		{
			name:           "201 with action that succeeds",
			deleteStatus:   http.StatusCreated,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{200, "success"}},
			wantDeleted:    1,
			wantActionGets: 1,
		},
		{
			name:           "action fails",
			deleteStatus:   http.StatusOK,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{200, "error"}},
			actionError:    `{"code":"protected","message":"load balancer is delete protected"}`,
			wantErr:        "action 42 failed: protected: load balancer is delete protected",
			wantActionGets: 1,
		},
		{
			name:           "poll retries 429 and 5xx",
			deleteStatus:   http.StatusOK,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{http.StatusTooManyRequests, ""}, {http.StatusBadGateway, ""}, {http.StatusGatewayTimeout, ""}, {200, "success"}},
			wantDeleted:    1,
			wantActionGets: 4,
		},
		{
			name:           "poll stops on other 4xx",
			deleteStatus:   http.StatusOK,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{http.StatusForbidden, ""}},
			wantErr:        "failed to get action 42: hetzner API returned 403 getting action",
			wantActionGets: 1,
		},
		{
			name:          "poll keeps failing until the deadline",
			deleteStatus:  http.StatusOK,
			deleteBody:    deleteAccepted,
			actionReplies: []actionReply{{http.StatusServiceUnavailable, ""}},
			wantErr:       "hetzner API returned 503 getting action",
		},
		{
			name:          "action never finishes, deadline hit mid-poll",
			deleteStatus:  http.StatusOK,
			deleteBody:    deleteAccepted,
			actionReplies: []actionReply{{200, "running"}},
			wantErr:       `timed out waiting for action 42 (last status "running")`,
		},
		{
			name:           "action never finishes, deadline hit between polls",
			deleteStatus:   http.StatusOK,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{200, "running"}},
			pollInterval:   time.Hour,
			timeout:        50 * time.Millisecond,
			wantErr:        `timed out waiting for action 42 (last status "running")`,
			wantActionGets: 1,
		},
		{
			name:           "caller cancels while waiting",
			deleteStatus:   http.StatusOK,
			deleteBody:     deleteAccepted,
			actionReplies:  []actionReply{{200, "running"}},
			pollInterval:   time.Hour,
			timeout:        time.Hour,
			cancelParent:   true,
			wantErr:        `stopped waiting for action 42 (last status "running"), the deletion may still be in progress: context canceled`,
			wantActionGets: 1,
		},
		{name: "server error", deleteStatus: http.StatusInternalServerError, wantErr: "hetzner API returned 500 deleting load balancer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HETZNER_TOKEN", "test-token")

			origInterval, origTimeout := actionPollInterval, actionTimeout
			actionPollInterval, actionTimeout = time.Millisecond, 200*time.Millisecond
			if tt.pollInterval != 0 {
				actionPollInterval = tt.pollInterval
			}
			if tt.timeout != 0 {
				actionTimeout = tt.timeout
			}
			defer func() { actionPollInterval, actionTimeout = origInterval, origTimeout }()

			updates := make(chan status.Update, 16)
			ctx, cancel := context.WithCancel(status.WithChannel(context.Background(), updates))
			defer cancel()

			lbs := loadBalancerListResponse{
				LoadBalancers: []hetznerLoadBalancer{
					{ID: 10, Name: "lb-orphan", Labels: map[string]string{"hcloud-ccm/service-uid": "abc"}, Targets: []any{}},
				},
			}

			var actionGets atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/load_balancers" && r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode(lbs)
				case r.URL.Path == "/load_balancers/10" && r.Method == http.MethodDelete:
					w.WriteHeader(tt.deleteStatus)
					_, _ = w.Write([]byte(tt.deleteBody))
				case r.URL.Path == "/actions/42" && r.Method == http.MethodGet:
					n := int(actionGets.Add(1))
					if tt.cancelParent && n == 1 {
						// Cancel once this poll has been answered, so the wait is
						// interrupted between polls rather than mid-request.
						time.AfterFunc(20*time.Millisecond, cancel)
					}
					reply := tt.actionReplies[min(n-1, len(tt.actionReplies)-1)]
					if reply.code != http.StatusOK {
						w.WriteHeader(reply.code)
						return
					}
					actionErr := "null"
					if tt.actionError != "" {
						actionErr = tt.actionError
					}
					_, _ = fmt.Fprintf(w, `{"action":{"id":42,"status":%q,"error":%s}}`, reply.status, actionErr)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			origBase := hetznerAPIBase
			setHetznerAPIBase(server.URL)
			defer setHetznerAPIBase(origBase)

			deleted, err := cleanupLoadBalancers(ctx)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("cleanupLoadBalancers() error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("cleanupLoadBalancers() error = %v", err)
			}
			if deleted != tt.wantDeleted {
				t.Errorf("deleted = %d, want %d", deleted, tt.wantDeleted)
			}
			if got := int(actionGets.Load()); tt.wantActionGets != 0 && got != tt.wantActionGets {
				t.Errorf("action GETs = %d, want %d", got, tt.wantActionGets)
			}

			waited := false
			close(updates)
			for u := range updates {
				if strings.Contains(u.Message, "Waiting for Hetzner to delete load balancer 10") {
					waited = true
				}
			}
			if wantWait := tt.deleteBody != ""; waited != wantWait {
				t.Errorf("sent wait status update = %v, want %v", waited, wantWait)
			}
		})
	}
}
