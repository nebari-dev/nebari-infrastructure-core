package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// DiscoverCluster discovers an EKS cluster by name and validates NIC tags
func (p *Provider) DiscoverCluster(ctx context.Context, clients *Clients, clusterName string) (*ClusterState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.DiscoverCluster")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	// Describe the cluster
	describeInput := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	describeOutput, err := clients.EKSClient.DescribeCluster(ctx, describeInput)
	if err != nil {
		// Check if cluster doesn't exist (not an error for stateless discovery)
		// AWS SDK returns ResourceNotFoundException
		span.RecordError(err)
		return nil, fmt.Errorf("failed to describe EKS cluster %s: %w", clusterName, err)
	}

	// Validate that the cluster is managed by NIC
	cluster := describeOutput.Cluster
	if cluster.Tags == nil {
		span.SetAttributes(attribute.Bool("managed_by_nic", false))
		return nil, fmt.Errorf("cluster %s exists but is not managed by NIC (no tags)", clusterName)
	}

	managedBy, ok := cluster.Tags[TagManagedBy]
	if !ok || managedBy != ManagedByValue {
		span.SetAttributes(attribute.Bool("managed_by_nic", false))
		return nil, fmt.Errorf("cluster %s exists but is not managed by NIC (missing or incorrect %s tag)", clusterName, TagManagedBy)
	}

	clusterNameTag, ok := cluster.Tags[TagClusterName]
	if !ok || clusterNameTag != clusterName {
		span.SetAttributes(attribute.Bool("managed_by_nic", false))
		return nil, fmt.Errorf("cluster %s has mismatched cluster name tag", clusterName)
	}

	span.SetAttributes(
		attribute.Bool("managed_by_nic", true),
		attribute.String("cluster_status", string(cluster.Status)),
	)

	// Convert to ClusterState
	clusterState := convertEKSClusterToState(cluster)

	return clusterState, nil
}
