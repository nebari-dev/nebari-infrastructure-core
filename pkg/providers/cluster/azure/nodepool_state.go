package azure

import (
	"context"
	"sort"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// clusterStateAddr is the managed cluster in the aks_cluster module. Its
	// default_node_pool is the system pool; every other node group is a
	// separate azurerm_kubernetes_cluster_node_pool.user[<name>].
	clusterStateAddr = "module.aks_cluster.azurerm_kubernetes_cluster.this"

	// userNodePoolStatePrefix matches every user node pool, whatever its key.
	//
	// Verified against terraform-azurerm-aks-cluster v0.2.0, the version pinned
	// in templates/main.tf. Re-check on every module bump.
	userNodePoolStatePrefix = "module.aks_cluster.azurerm_kubernetes_cluster_node_pool.user["
)

// nodePoolStateEditor is the subset of the tofu executor releaseUserNodePools
// needs. *tofu.TerraformExecutor satisfies it.
type nodePoolStateEditor interface {
	Show(ctx context.Context) (*tfjson.State, error)
	StateRm(ctx context.Context, address string) error
}

// releaseUserNodePools drops the user node pools from Terraform state so that
// `tofu destroy` deletes them together with the managed cluster instead of one
// by one.
//
// Deleting an agent pool on its own makes AKS drain its nodes, and the drain
// honors PodDisruptionBudgets. A PDB that can never be satisfied (for example
// the primary PDB of a single-instance CloudNativePG cluster) blocks the drain
// until the azurerm delete timeout, an hour later, and leaves the whole cluster
// running. Deleting the managed cluster removes every agent pool without
// draining, which is what a teardown wants.
//
// Pools are only released when the cluster itself is in state, so the destroy
// that follows is guaranteed to take them with it. If the run is interrupted
// between this step and the cluster delete, the pools stay in Azure but out of
// state: re-running `nic destroy` cleans them up with the cluster.
//
// It is best-effort and never fails the teardown (Destroy Error Contract,
// behavior 3): when state cannot be read or a removal fails, the pool stays in
// state and tofu falls back to deleting it on its own, which is the behavior
// before this step existed and leaks nothing.
func releaseUserNodePools(ctx context.Context, tf nodePoolStateEditor) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.releaseUserNodePools")
	defer span.End()

	state, err := tf.Show(ctx)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not inspect Terraform state; user node pools will be deleted individually and may wait on PodDisruptionBudgets").
			WithResource("node-pool").WithAction("release").
			WithMetadata("error", err.Error()))
		return
	}

	present := cluster.PresentStateAddresses(state)
	if !present[clusterStateAddr] {
		return
	}

	var pools []string
	for addr := range present {
		if strings.HasPrefix(addr, userNodePoolStatePrefix) {
			pools = append(pools, addr)
		}
	}
	sort.Strings(pools)
	span.SetAttributes(attribute.Int("user_node_pools", len(pools)))

	for _, addr := range pools {
		if err := tf.StateRm(ctx, addr); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not remove user node pool from Terraform state; it will be deleted individually and may wait on PodDisruptionBudgets").
				WithResource("node-pool").WithAction("release").
				WithMetadata("error", err.Error()).WithMetadata("address", addr))
			continue
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "User node pool will be deleted with the cluster (removed from Terraform state)").
			WithResource("node-pool").WithAction("release").
			WithMetadata("address", addr))
	}
}
