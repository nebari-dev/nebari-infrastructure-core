package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

// deleteNodeGroups deletes all node groups for a cluster
func (p *Provider) deleteNodeGroups(ctx context.Context, clients *Clients, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteNodeGroups")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	// Discover all node groups for this cluster
	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Discovering node groups").
		WithResource("node-group").
		WithAction("discovering"))

	nodeGroups, err := p.DiscoverNodeGroups(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to discover node groups: %w", err)
	}

	if len(nodeGroups) == 0 {
		span.SetAttributes(attribute.Int("node_groups_deleted", 0))
		status.Send(ctx, status.NewStatusUpdate(status.LevelInfo, "No node groups to delete").
			WithResource("node-group"))
		return nil
	}

	span.SetAttributes(attribute.Int("node_groups_to_delete", len(nodeGroups)))
	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Deleting node groups").
		WithResource("node-group").
		WithAction("deleting").
		WithMetadata("count", len(nodeGroups)))

	// Delete node groups in parallel using errgroup
	g, gctx := errgroup.WithContext(ctx)

	for _, ng := range nodeGroups {
		ngName := ng.Name // Capture for goroutine

		g.Go(func() error {
			return p.deleteNodeGroup(gctx, clients, clusterName, ngName)
		})
	}

	// Wait for all deletions to complete
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.Int("node_groups_deleted", len(nodeGroups)),
		attribute.Bool("parallel_deletion", true),
	)

	status.Send(ctx, status.NewStatusUpdate(status.LevelSuccess, "Node groups deleted").
		WithResource("node-group").
		WithAction("deleted").
		WithMetadata("count", len(nodeGroups)))

	return nil
}

// deleteNodeGroup deletes a single node group and waits for completion
func (p *Provider) deleteNodeGroup(ctx context.Context, clients *Clients, clusterName, nodeGroupName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteNodeGroup")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("node_group_name", nodeGroupName),
	)

	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Deleting node group").
		WithResource("node-group").
		WithAction("deleting").
		WithMetadata("node_group", nodeGroupName))

	// Initiate deletion
	_, err := clients.EKSClient.DeleteNodegroup(ctx, &eks.DeleteNodegroupInput{
		ClusterName:   &clusterName,
		NodegroupName: &nodeGroupName,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete node group %s: %w", nodeGroupName, err)
	}

	// Wait for deletion to complete
	waiter := eks.NewNodegroupDeletedWaiter(clients.EKSClient)
	waitCtx, cancel := context.WithTimeout(ctx, NodeGroupDeleteTimeout)
	defer cancel()

	err = waiter.Wait(waitCtx, &eks.DescribeNodegroupInput{
		ClusterName:   &clusterName,
		NodegroupName: &nodeGroupName,
	}, NodeGroupDeleteTimeout)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed waiting for node group %s deletion: %w", nodeGroupName, err)
	}

	span.SetAttributes(attribute.Bool("deletion_complete", true))

	status.Send(ctx, status.NewStatusUpdate(status.LevelSuccess, "Node group deleted").
		WithResource("node-group").
		WithAction("deleted").
		WithMetadata("node_group", nodeGroupName))

	return nil
}
