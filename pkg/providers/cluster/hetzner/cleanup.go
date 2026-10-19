package hetzner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var hetznerAPIBase = "https://api.hetzner.cloud/v1"

const persistLabel = "true"

// setHetznerAPIBase overrides the API base URL (for testing).
func setHetznerAPIBase(url string) {
	hetznerAPIBase = url
}

// hetznerVolume represents a volume from the Hetzner Cloud API.
type hetznerVolume struct {
	ID     int               `json:"id"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Server *int              `json:"server"`
}

type volumeListResponse struct {
	Volumes []hetznerVolume `json:"volumes"`
}

type hetznerServer struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

type serverResponse struct {
	Server *hetznerServer `json:"server"`
}

func newHetznerClient() (*http.Client, string, error) {
	token := os.Getenv("HETZNER_TOKEN")
	if token == "" {
		return nil, "", fmt.Errorf("HETZNER_TOKEN not set")
	}
	return &http.Client{Timeout: 30 * time.Second}, token, nil
}

// labelPersistVolumes adds persist=true to all CSI-managed volumes attached to
// servers in this cluster. Called during deploy when persist_data is true.
func labelPersistVolumes(ctx context.Context) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.labelPersistVolumes")
	defer span.End()

	client, token, err := newHetznerClient()
	if err != nil {
		return err
	}

	volumes, err := listCSIVolumes(ctx, client, token)
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(attribute.Int("volume_count", len(volumes)))

	labeled := 0
	for _, vol := range volumes {
		if vol.Labels["persist"] == persistLabel {
			continue
		}
		if err := addVolumeLabel(ctx, client, token, vol); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to label volume %d (%s): %w", vol.ID, vol.Name, err)
		}
		labeled++
	}

	span.SetAttributes(attribute.Int("volumes_labeled", labeled))
	return nil
}

// cleanupVolumes deletes CSI-managed volumes that are not marked persist=true
// and are not attached to a running server. Called during destroy.
func cleanupVolumes(ctx context.Context) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.cleanupVolumes")
	defer span.End()

	client, token, err := newHetznerClient()
	if err != nil {
		return 0, err
	}

	volumes, err := listCSIVolumes(ctx, client, token)
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to list volumes: %w", err)
	}

	span.SetAttributes(attribute.Int("total_csi_volumes", len(volumes)))

	deleted := 0
	for _, vol := range volumes {
		// Skip volumes marked for persistence
		if vol.Labels["persist"] == persistLabel {
			continue
		}

		// Skip volumes attached to a running server
		if vol.Server != nil {
			running, err := isServerRunning(ctx, client, token, *vol.Server)
			if err != nil {
				span.RecordError(err)
				return deleted, fmt.Errorf("failed to check server %d for volume %d: %w", *vol.Server, vol.ID, err)
			}
			if running {
				continue
			}

			// Detach from non-running server before deleting
			if err := detachVolume(ctx, client, token, vol.ID); err != nil {
				span.RecordError(err)
				return deleted, fmt.Errorf("failed to detach volume %d (%s): %w", vol.ID, vol.Name, err)
			}
		}

		if err := deleteVolume(ctx, client, token, vol.ID); err != nil {
			span.RecordError(err)
			return deleted, fmt.Errorf("failed to delete volume %d (%s): %w", vol.ID, vol.Name, err)
		}
		deleted++
	}

	span.SetAttributes(attribute.Int("volumes_deleted", deleted))
	return deleted, nil
}

// listCSIVolumes returns all volumes with the managed-by=csi-driver label.
func listCSIVolumes(ctx context.Context, client *http.Client, token string) ([]hetznerVolume, error) {
	url := hetznerAPIBase + "/volumes?label_selector=managed-by=csi-driver"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hetzner API returned %d listing volumes", resp.StatusCode)
	}

	var result volumeListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode volume list: %w", err)
	}

	return result.Volumes, nil
}

// addVolumeLabel adds persist=true to a volume's existing labels.
func addVolumeLabel(ctx context.Context, client *http.Client, token string, vol hetznerVolume) error {
	labels := make(map[string]string)
	for k, v := range vol.Labels {
		labels[k] = v
	}
	labels["persist"] = persistLabel

	body, err := json.Marshal(map[string]any{"labels": labels})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/volumes/%d", hetznerAPIBase, vol.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hetzner API returned %d updating volume labels", resp.StatusCode)
	}

	return nil
}

// isServerRunning checks if a server exists and is in the "running" state.
func isServerRunning(ctx context.Context, client *http.Client, token string, serverID int) (bool, error) {
	url := fmt.Sprintf("%s/servers/%d", hetznerAPIBase, serverID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck

	// Server doesn't exist
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("hetzner API returned %d checking server %d", resp.StatusCode, serverID)
	}

	var result serverResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	return result.Server != nil && result.Server.Status == "running", nil
}

func detachVolume(ctx context.Context, client *http.Client, token string, volumeID int) error {
	url := fmt.Sprintf("%s/volumes/%d/actions/detach", hetznerAPIBase, volumeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	// 409 means already detached
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("hetzner API returned %d detaching volume", resp.StatusCode)
	}

	return nil
}

func deleteVolume(ctx context.Context, client *http.Client, token string, volumeID int) error {
	url := fmt.Sprintf("%s/volumes/%d", hetznerAPIBase, volumeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("hetzner API returned %d deleting volume", resp.StatusCode)
	}

	return nil
}

// --- Load Balancer Cleanup ---

// hetznerLoadBalancer represents a load balancer from the Hetzner Cloud API.
type hetznerLoadBalancer struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	Labels  map[string]string `json:"labels"`
	Targets []any             `json:"targets"`
}

type loadBalancerListResponse struct {
	LoadBalancers []hetznerLoadBalancer `json:"load_balancers"`
}

// cleanupLoadBalancers deletes CCM-managed load balancers that have no targets
// (orphaned after cluster destruction). Called during destroy.
func cleanupLoadBalancers(ctx context.Context) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.cleanupLoadBalancers")
	defer span.End()

	client, token, err := newHetznerClient()
	if err != nil {
		return 0, err
	}

	lbs, err := listOrphanedLoadBalancers(ctx, client, token)
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to list load balancers: %w", err)
	}

	span.SetAttributes(attribute.Int("orphaned_lbs", len(lbs)))

	deleted := 0
	for _, lb := range lbs {
		if err := deleteLoadBalancer(ctx, client, token, lb.ID); err != nil {
			span.RecordError(err)
			return deleted, fmt.Errorf("failed to delete load balancer %d (%s): %w", lb.ID, lb.Name, err)
		}
		deleted++
	}

	span.SetAttributes(attribute.Int("lbs_deleted", deleted))
	return deleted, nil
}

// listOrphanedLoadBalancers returns CCM-managed load balancers with no targets.
func listOrphanedLoadBalancers(ctx context.Context, client *http.Client, token string) ([]hetznerLoadBalancer, error) {
	url := hetznerAPIBase + "/load_balancers?label_selector=hcloud-ccm/service-uid"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hetzner API returned %d listing load balancers", resp.StatusCode)
	}

	var result loadBalancerListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode load balancer list: %w", err)
	}

	var orphaned []hetznerLoadBalancer
	for _, lb := range result.LoadBalancers {
		if len(lb.Targets) == 0 {
			orphaned = append(orphaned, lb)
		}
	}

	return orphaned, nil
}

func deleteLoadBalancer(ctx context.Context, client *http.Client, token string, lbID int) error {
	url := fmt.Sprintf("%s/load_balancers/%d", hetznerAPIBase, lbID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	// Since 2026-10-05 the API answers with an Action instead of an empty 204
	// (documented as 201, observed as 200), so accept any 2xx and wait for it.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("hetzner API returned %d deleting load balancer", resp.StatusCode)
	}

	var result actionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to decode load balancer delete response: %w", err)
	}
	if result.Action == nil {
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Waiting for Hetzner to delete load balancer %d", lbID)).
		WithResource("provider").WithAction("destroy"))

	return waitForAction(ctx, client, token, result.Action.ID)
}

// --- Actions ---

// hetznerAction represents an asynchronous operation from the Hetzner Cloud API.
type hetznerAction struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type actionResponse struct {
	Action *hetznerAction `json:"action"`
}

var (
	actionPollInterval = 2 * time.Second
	actionTimeout      = 5 * time.Minute
)

// errTransient marks a failed poll that is worth retrying: a network error,
// a 429, or a 5xx (the API documents 502 and 504 as "please retry").
var errTransient = errors.New("transient")

// waitForAction polls an action until it succeeds, fails, or actionTimeout
// elapses. Transient poll failures are retried until the deadline.
func waitForAction(ctx context.Context, client *http.Client, token string, actionID int64) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	parentCtx, span := tracer.Start(ctx, "hetzner.waitForAction")
	defer span.End()

	span.SetAttributes(attribute.Int64("action_id", actionID))

	ctx, cancel := context.WithTimeout(parentCtx, actionTimeout)
	defer cancel()

	lastStatus := "unknown"
	var lastErr error
	stopped := func() error {
		var err error
		if parentCtx.Err() != nil {
			err = fmt.Errorf("stopped waiting for action %d (last status %q), the deletion may still be in progress: %w", actionID, lastStatus, parentCtx.Err())
		} else {
			err = fmt.Errorf("timed out waiting for action %d (last status %q): %w", actionID, lastStatus, ctx.Err())
			if lastErr != nil {
				err = fmt.Errorf("%w (last poll error: %v)", err, lastErr)
			}
		}
		span.RecordError(err)
		return err
	}
	for {
		action, err := getAction(ctx, client, token, actionID)
		switch {
		case err != nil && ctx.Err() != nil:
			return stopped()
		case err != nil && errors.Is(err, errTransient):
			lastErr = err
		case err != nil:
			span.RecordError(err)
			return fmt.Errorf("failed to get action %d: %w", actionID, err)
		default:
			lastErr = nil
			lastStatus = action.Status
			switch action.Status {
			case "success":
				return nil
			case "error":
				err := fmt.Errorf("action %d failed", actionID)
				if action.Error != nil {
					err = fmt.Errorf("action %d failed: %s: %s", actionID, action.Error.Code, action.Error.Message)
				}
				span.RecordError(err)
				return err
			}
		}

		select {
		case <-ctx.Done():
			return stopped()
		case <-time.After(actionPollInterval):
		}
	}
}

func getAction(ctx context.Context, client *http.Client, token string, actionID int64) (*hetznerAction, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.getAction")
	defer span.End()

	span.SetAttributes(attribute.Int64("action_id", actionID))

	url := fmt.Sprintf("%s/actions/%d", hetznerAPIBase, actionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("%w: %w", errTransient, err)
		span.RecordError(err)
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		err := fmt.Errorf("%w: hetzner API returned %d getting action", errTransient, resp.StatusCode)
		span.RecordError(err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("hetzner API returned %d getting action", resp.StatusCode)
		span.RecordError(err)
		return nil, err
	}

	var result actionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		err = fmt.Errorf("failed to decode action: %w", err)
		span.RecordError(err)
		return nil, err
	}
	if result.Action == nil {
		err := fmt.Errorf("hetzner API returned no action")
		span.RecordError(err)
		return nil, err
	}

	return result.Action, nil
}
