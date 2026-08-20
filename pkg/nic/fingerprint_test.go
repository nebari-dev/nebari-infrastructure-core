package nic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/fingerprint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// countingClusterProvider records how many times GetKubeconfig was asked for, so
// a test can tell "skipped" apart from "attempted and failed".
type countingClusterProvider struct {
	fakeClusterProvider
	kubeconfigCalls int
}

func (p *countingClusterProvider) GetKubeconfig(context.Context, string, *config.ClusterConfig) ([]byte, error) {
	p.kubeconfigCalls++
	return nil, errors.New("cluster unreachable")
}

// collectStatus attaches a buffered status channel to ctx and returns the
// updates sent during fn. The buffer is generous so a producer never blocks.
func collectStatus(ctx context.Context, fn func(context.Context)) []status.Update {
	ch := make(chan status.Update, 32)
	fn(status.WithChannel(ctx, ch))
	close(ch)

	var updates []status.Update
	for u := range ch {
		updates = append(updates, u)
	}
	return updates
}

// TestRecordFingerprintSkippedWithoutBuild pins the WithBuild contract: a Client
// built without a build identity records nothing at all, rather than stamping a
// cluster with placeholder provenance. Asserted by the absence of a kubeconfig
// fetch, so the test fails if the skip moves after the cluster round-trip.
func TestRecordFingerprintSkippedWithoutBuild(t *testing.T) {
	provider := &countingClusterProvider{}
	client := &Client{}
	cfg := &config.NebariConfig{ProjectName: "test"}

	updates := collectStatus(context.Background(), func(ctx context.Context) {
		client.recordFingerprint(ctx, cfg, provider)
	})

	if provider.kubeconfigCalls != 0 {
		t.Errorf("GetKubeconfig called %d times, want 0 when no build identity is set", provider.kubeconfigCalls)
	}
	if len(updates) != 0 {
		t.Errorf("got %d status updates, want none: %v", len(updates), updates)
	}
}

// TestRecordFingerprintWarnsOnFailure pins the warn-and-continue contract. The
// metadata write is provenance, not infrastructure: a cluster that came up must
// not be reported as a failed deploy because the ConfigMap could not be written.
// recordFingerprint returns nothing, so the warning is the only signal the
// operator gets and it has to name what was lost.
func TestRecordFingerprintWarnsOnFailure(t *testing.T) {
	provider := &countingClusterProvider{}
	client := &Client{build: &fingerprint.Build{Version: "v0.13.0", Commit: "abc1234"}}
	cfg := &config.NebariConfig{ProjectName: "test"}

	updates := collectStatus(context.Background(), func(ctx context.Context) {
		client.recordFingerprint(ctx, cfg, provider)
	})

	if provider.kubeconfigCalls != 1 {
		t.Errorf("GetKubeconfig called %d times, want 1", provider.kubeconfigCalls)
	}

	var warned bool
	for _, u := range updates {
		if u.Level != status.LevelWarning {
			continue
		}
		warned = true
		// The message must name the ConfigMap, otherwise the operator cannot
		// tell which record is missing.
		if !strings.Contains(u.Message, fingerprint.Name) {
			t.Errorf("warning %q does not name the ConfigMap %q", u.Message, fingerprint.Name)
		}
		if got, ok := u.Metadata["error"].(string); !ok || !strings.Contains(got, "cluster unreachable") {
			t.Errorf("warning metadata error = %v, want the underlying cause", u.Metadata["error"])
		}
	}
	if !warned {
		t.Errorf("no warning emitted; got %v", updates)
	}
}

// TestWithBuildSetsIdentity covers the option itself, including that the zero
// Client (no option) leaves the identity nil, which is what the skip above keys
// on.
func TestWithBuildSetsIdentity(t *testing.T) {
	var c Client
	if c.build != nil {
		t.Fatalf("build = %v on a zero Client, want nil", c.build)
	}

	WithBuild("v1.2.3", "deadbee", "2026-08-19T00:00:00Z")(&c)

	want := fingerprint.Build{Version: "v1.2.3", Commit: "deadbee", Date: "2026-08-19T00:00:00Z"}
	if c.build == nil || *c.build != want {
		t.Errorf("build = %v, want %v", c.build, want)
	}
}

// compile-time assertion that the counting fake still satisfies the interface it
// stands in for.
var _ cluster.Provider = (*countingClusterProvider)(nil)
