package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// deleteEKSCluster deletes the EKS cluster and waits for completion
func (p *Provider) deleteEKSCluster(ctx context.Context, clients *Clients, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteEKSCluster")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Checking EKS cluster").
		WithResource("eks-cluster").
		WithAction("discovering"))

	// Try to discover the cluster first
	cluster, err := p.DiscoverCluster(ctx, clients, clusterName)
	if err != nil {
		// Cluster doesn't exist - nothing to delete
		span.SetAttributes(attribute.Bool("cluster_exists", false))
		status.Send(ctx, status.NewStatusUpdate(status.LevelInfo, "EKS cluster not found").
			WithResource("eks-cluster"))
		return nil
	}

	if cluster == nil {
		// Cluster doesn't exist - nothing to delete
		span.SetAttributes(attribute.Bool("cluster_exists", false))
		status.Send(ctx, status.NewStatusUpdate(status.LevelInfo, "EKS cluster not found").
			WithResource("eks-cluster"))
		return nil
	}

	span.SetAttributes(attribute.Bool("cluster_exists", true))

	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Deleting EKS cluster").
		WithResource("eks-cluster").
		WithAction("deleting").
		WithMetadata("cluster_name", clusterName))

	// Initiate deletion
	_, err = clients.EKSClient.DeleteCluster(ctx, &eks.DeleteClusterInput{
		Name: &clusterName,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete EKS cluster %s: %w", clusterName, err)
	}

	// Wait for deletion to complete
	waiter := eks.NewClusterDeletedWaiter(clients.EKSClient)
	waitCtx, cancel := context.WithTimeout(ctx, EKSClusterDeleteTimeout)
	defer cancel()

	err = waiter.Wait(waitCtx, &eks.DescribeClusterInput{
		Name: &clusterName,
	}, EKSClusterDeleteTimeout)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed waiting for EKS cluster %s deletion: %w", clusterName, err)
	}

	span.SetAttributes(attribute.Bool("deletion_complete", true))

	status.Send(ctx, status.NewStatusUpdate(status.LevelSuccess, "EKS cluster deleted").
		WithResource("eks-cluster").
		WithAction("deleted").
		WithMetadata("cluster_name", clusterName))

	return nil
}
