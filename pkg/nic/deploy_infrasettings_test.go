package nic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	repositorylocal "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository/local"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// refreshingClusterProvider is a fake cluster.Provider whose InfraSettings
// returns a different MetalLB address pool before and after Deploy runs. It
// stands in for the real local (kind) provider, which derives its MetalLB
// pool from the live kind node network during Deploy and only has it
// available afterward.
type refreshingClusterProvider struct {
	deployed bool
}

func (f *refreshingClusterProvider) Name() string { return "fake" }

func (f *refreshingClusterProvider) Validate(context.Context, string, *config.ClusterConfig) error {
	return nil
}

func (f *refreshingClusterProvider) Deploy(context.Context, string, *config.ClusterConfig, cluster.DeployOptions) error {
	f.deployed = true
	return nil
}

func (f *refreshingClusterProvider) Destroy(context.Context, string, *config.ClusterConfig, cluster.DestroyOptions) error {
	return nil
}

func (f *refreshingClusterProvider) GetKubeconfig(context.Context, string, *config.ClusterConfig) ([]byte, error) {
	return nil, errors.New("no kubeconfig in test")
}

func (f *refreshingClusterProvider) Summary(*config.ClusterConfig) map[string]string {
	return nil
}

// InfraSettings returns the pre-Deploy fallback pool until Deploy has run,
// then the "derived" pool afterward. This mirrors the local provider's real
// contract: metalLBPool is empty until Deploy populates it.
func (f *refreshingClusterProvider) InfraSettings(*config.ClusterConfig) cluster.InfraSettings {
	pool := "192.168.1.100-192.168.1.110"
	if f.deployed {
		pool = "172.18.255.100-172.18.255.110"
	}
	return cluster.InfraSettings{
		StorageClass:        "standard",
		NeedsMetalLB:        true,
		MetalLBAddressPool:  pool,
		SupportsLocalGitOps: true,
	}
}

// TestDeployUsesPostDeployInfraSettings pins the fix for #612: InfraSettings
// must be re-read after Deploy so runtime-derived values (like the local
// provider's kind-network MetalLB pool) actually reach the GitOps manifests.
// Before the fix, Deploy read InfraSettings only once, before Deploy ran, so
// the local provider's derived pool was always thrown away in favor of its
// unroutable static fallback. The MetalLB IPAddressPool manifest written to
// the GitOps repo is the observable proof: it must contain the post-Deploy
// pool, not the pre-Deploy one.
func TestDeployUsesPostDeployInfraSettings(t *testing.T) {
	tests := []struct {
		name     string
		wantPool string
	}{
		{
			name:     "gitops manifest reflects post-deploy derived pool",
			wantPool: "172.18.255.100-172.18.255.110",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repoDir := t.TempDir()

			reg := registry.NewRegistry()
			fake := &refreshingClusterProvider{}
			if err := reg.ClusterProviders.Register(ctx, "fake", fake); err != nil {
				t.Fatalf("register fake cluster provider: %v", err)
			}
			if err := reg.RepositoryProviders.Register(ctx, repositorylocal.ProviderName, repositorylocal.NewProvider()); err != nil {
				t.Fatalf("register local repository provider: %v", err)
			}

			client := &Client{registry: reg}

			cfg := &config.NebariConfig{
				ProjectName: "test-project",
				Cluster: &config.ClusterConfig{
					Providers: map[string]any{"fake": map[string]any{}},
				},
				Repository: &config.RepositoryConfig{
					Providers: map[string]any{
						repositorylocal.ProviderName: map[string]any{"path": repoDir},
					},
				},
			}

			result, err := client.Deploy(ctx, cfg, DeployOptions{DryRun: false})
			if err != nil {
				t.Fatalf("Deploy() unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("Deploy() returned nil result")
			}
			if !fake.deployed {
				t.Fatal("fake cluster provider Deploy was never called")
			}

			poolManifest := filepath.Join(repoDir, "manifests", "metallb", "ipaddresspool.yaml")
			contents, err := os.ReadFile(poolManifest)
			if err != nil {
				t.Fatalf("read written metallb manifest: %v", err)
			}

			if !strings.Contains(string(contents), tt.wantPool) {
				t.Errorf("metallb manifest = %q, want it to contain post-deploy pool %q", contents, tt.wantPool)
			}
			if strings.Contains(string(contents), "192.168.1.100-192.168.1.110") {
				t.Errorf("metallb manifest still contains the pre-deploy fallback pool:\n%s", contents)
			}
		})
	}
}
