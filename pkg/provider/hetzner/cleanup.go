package hetzner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

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

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("hetzner API returned %d deleting load balancer", resp.StatusCode)
	}

	return nil
}
