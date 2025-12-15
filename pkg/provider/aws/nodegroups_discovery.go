package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// DiscoverNodeGroups discovers all EKS node groups for a cluster and validates NIC tags
func (p *Provider) DiscoverNodeGroups(ctx context.Context, clients *Clients, clusterName string) ([]NodeGroupState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.DiscoverNodeGroups")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	// List all node groups for this cluster with pagination
	var allNodeGroups []string
	var nextToken *string

	for {
		listInput := &eks.ListNodegroupsInput{
			ClusterName: aws.String(clusterName),
			NextToken:   nextToken,
		}

		listOutput, err := clients.EKSClient.ListNodegroups(ctx, listInput)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to list node groups for cluster %s: %w", clusterName, err)
		}

		allNodeGroups = append(allNodeGroups, listOutput.Nodegroups...)

		if listOutput.NextToken == nil {
			break
		}
		nextToken = listOutput.NextToken
	}

	if len(allNodeGroups) == 0 {
		span.SetAttributes(attribute.Int("node_group_count", 0))
		return []NodeGroupState{}, nil
	}

	span.SetAttributes(
		attribute.Int("node_group_count", len(allNodeGroups)),
	)

	nodeGroupStates := make([]NodeGroupState, 0, len(allNodeGroups))

	// Describe each node group and validate NIC tags
	for _, nodeGroupName := range allNodeGroups {
		describeInput := &eks.DescribeNodegroupInput{
			ClusterName:   aws.String(clusterName),
			NodegroupName: aws.String(nodeGroupName),
		}

		describeOutput, err := clients.EKSClient.DescribeNodegroup(ctx, describeInput)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to describe node group %s: %w", nodeGroupName, err)
		}

		nodeGroup := describeOutput.Nodegroup

		// Validate that the node group is managed by NIC
		if nodeGroup.Tags == nil {
			span.SetAttributes(
				attribute.String(fmt.Sprintf("node_group.%s.managed_by_nic", nodeGroupName), "false"),
			)
			continue // Skip node groups not managed by NIC
		}

		managedBy, ok := nodeGroup.Tags[TagManagedBy]
		if !ok || managedBy != ManagedByValue {
			span.SetAttributes(
				attribute.String(fmt.Sprintf("node_group.%s.managed_by_nic", nodeGroupName), "false"),
			)
			continue // Skip node groups not managed by NIC
		}

		clusterNameTag, ok := nodeGroup.Tags[TagClusterName]
		if !ok || clusterNameTag != clusterName {
			span.SetAttributes(
				attribute.String(fmt.Sprintf("node_group.%s.managed_by_nic", nodeGroupName), "false"),
			)
			continue // Skip node groups with mismatched cluster name
		}

		span.SetAttributes(
			attribute.String(fmt.Sprintf("node_group.%s.managed_by_nic", nodeGroupName), "true"),
			attribute.String(fmt.Sprintf("node_group.%s.status", nodeGroupName), string(nodeGroup.Status)),
		)

		// Convert to NodeGroupState
		nodeGroupState := convertEKSNodeGroupToState(nodeGroup)
		nodeGroupStates = append(nodeGroupStates, *nodeGroupState)
	}

	span.SetAttributes(
		attribute.Int("nic_managed_node_groups", len(nodeGroupStates)),
	)

	return nodeGroupStates, nil
}
