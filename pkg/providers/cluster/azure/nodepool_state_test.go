package azure

import (
	"context"
	"errors"
	"slices"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// fakeNodePoolState serves a canned Show result and records StateRm calls.
type fakeNodePoolState struct {
	state    *tfjson.State
	showErr  error
	rmErrFor map[string]error
	rmCalls  []string
}

func (f *fakeNodePoolState) Show(_ context.Context) (*tfjson.State, error) {
	return f.state, f.showErr
}

func (f *fakeNodePoolState) StateRm(_ context.Context, address string) error {
	f.rmCalls = append(f.rmCalls, address)
	return f.rmErrFor[address]
}

// aksModuleState builds a state shaped like the real one: the given addresses
// live in the aks_cluster child module, not the root module.
func aksModuleState(addrs ...string) *tfjson.State {
	resources := make([]*tfjson.StateResource, 0, len(addrs))
	for _, a := range addrs {
		resources = append(resources, &tfjson.StateResource{Address: a})
	}
	return &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				ChildModules: []*tfjson.StateModule{
					{Address: "module.aks_cluster", Resources: resources},
				},
			},
		},
	}
}

func TestReleaseUserNodePools(t *testing.T) {
	const (
		workerPool = `module.aks_cluster.azurerm_kubernetes_cluster_node_pool.user["worker"]`
		gpuPool    = `module.aks_cluster.azurerm_kubernetes_cluster_node_pool.user["gpu"]`
		vnet       = "module.aks_cluster.azurerm_virtual_network.this[0]"
		subnet     = "module.aks_cluster.azurerm_subnet.nodes[0]"
		nap        = "module.aks_cluster.azapi_update_resource.node_auto_provisioning[0]"
		subnetRole = "module.aks_cluster.azurerm_role_assignment.network_contributor[0]"
	)

	tests := []struct {
		name        string
		fake        *fakeNodePoolState
		wantRm      []string
		wantWarning bool
	}{
		{
			name:   "removes every user pool, sorted, and nothing else",
			fake:   &fakeNodePoolState{state: aksModuleState(vnet, subnet, nap, subnetRole, clusterStateAddr, workerPool, gpuPool)},
			wantRm: []string{gpuPool, workerPool},
		},
		{
			name:   "cluster without user pools removes nothing",
			fake:   &fakeNodePoolState{state: aksModuleState(vnet, clusterStateAddr)},
			wantRm: nil,
		},
		{
			// Without the cluster in state the destroy would not take the
			// pools with it, so they must stay in state.
			name:   "pools without the cluster stay in state",
			fake:   &fakeNodePoolState{state: aksModuleState(vnet, workerPool)},
			wantRm: nil,
		},
		{
			name:   "empty state removes nothing",
			fake:   &fakeNodePoolState{state: &tfjson.State{}},
			wantRm: nil,
		},
		{
			name:        "show failure warns and removes nothing",
			fake:        &fakeNodePoolState{showErr: errors.New("backend unreachable")},
			wantRm:      nil,
			wantWarning: true,
		},
		{
			name: "a failed removal warns and the rest are still removed",
			fake: &fakeNodePoolState{
				state:    aksModuleState(clusterStateAddr, workerPool, gpuPool),
				rmErrFor: map[string]error{gpuPool: errors.New("state locked")},
			},
			wantRm:      []string{gpuPool, workerPool},
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan status.Update, 16)
			ctx := status.WithChannel(context.Background(), ch)

			releaseUserNodePools(ctx, tt.fake)
			close(ch)

			if !slices.Equal(tt.fake.rmCalls, tt.wantRm) {
				t.Errorf("StateRm calls = %v, want %v", tt.fake.rmCalls, tt.wantRm)
			}

			gotWarning := false
			for u := range ch {
				if u.Level == status.LevelWarning {
					gotWarning = true
				}
			}
			if gotWarning != tt.wantWarning {
				t.Errorf("warning emitted = %v, want %v", gotWarning, tt.wantWarning)
			}
		})
	}
}
