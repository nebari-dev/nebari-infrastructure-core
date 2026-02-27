package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const k3sReleasesURL = "https://api.github.com/repos/k3s-io/k3s/releases"

// ghRelease is the subset of GitHub release API response we need.
type ghRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
}

// resolveK3sVersion resolves a Kubernetes version (e.g., "1.32" or "1.32.0")
// to a full k3s release tag (e.g., "v1.32.12+k3s1") by querying the GitHub API.
// If version already contains "+k3s", it's returned as-is.
// apiURL allows injecting a test server URL; pass "" to use the default GitHub API.
func resolveK3sVersion(ctx context.Context, version string, apiURL string) (string, error) {
	if strings.Contains(version, "+k3s") {
		return version, nil
	}

	if apiURL == "" {
		apiURL = k3sReleasesURL
	}

	normalized := strings.TrimPrefix(version, "v")
	parts := strings.Split(normalized, ".")

	var matchPrefix string
	switch len(parts) {
	case 2:
		matchPrefix = fmt.Sprintf("v%s.%s.", parts[0], parts[1])
	case 3:
		matchPrefix = fmt.Sprintf("v%s.%s.%s+", parts[0], parts[1], parts[2])
	default:
		return "", fmt.Errorf("invalid kubernetes version format: %q (expected MAJOR.MINOR or MAJOR.MINOR.PATCH)", version)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch k3s releases: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Best-effort close on read-only response

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var releases []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to decode k3s releases: %w", err)
	}

	for _, r := range releases {
		if r.Prerelease {
			continue
		}
		if strings.HasPrefix(r.TagName, matchPrefix) {
			return r.TagName, nil
		}
	}

	return "", fmt.Errorf("no stable k3s release found for kubernetes version %q", version)
}
