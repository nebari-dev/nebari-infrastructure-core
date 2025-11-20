package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// QueryWithRegion discovers the current state of AWS infrastructure in a specific region
// This is the actual implementation that Query() should delegate to
// Note: Pure orchestration function - delegates all logic to tested helper functions.
// Unit test coverage via helper functions. Integration tests validate orchestration.
func (p *Provider) QueryWithRegion(ctx context.Context, clusterName, region string) (*provider.InfrastructureState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.QueryWithRegion")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// Initialize AWS clients
	clients, err := newClientsFunc(ctx, region)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create AWS clients: %w", err)
	}

	// Discover all infrastructure components
	// Start with cluster discovery as it's the core resource
	cluster, err := p.DiscoverCluster(ctx, clients, clusterName)
	if err != nil {
		// Cluster doesn't exist - no infrastructure found
		span.SetAttributes(attribute.Bool("infrastructure_exists", false))
		return nil, nil
	}

	if cluster == nil {
		// Cluster doesn't exist - no infrastructure found
		span.SetAttributes(attribute.Bool("infrastructure_exists", false))
		return nil, nil
	}

	span.SetAttributes(attribute.Bool("infrastructure_exists", true))

	// Discover VPC
	vpc, err := p.DiscoverVPC(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		// Continue without VPC if discovery fails
		vpc = nil
	}

	// Discover node groups
	nodeGroups, err := p.DiscoverNodeGroups(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to discover node groups: %w", err)
	}

	// Discover IAM roles
	iamRoles, err := p.discoverIAMRoles(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		// Continue without IAM roles if discovery fails
		iamRoles = nil
	}

	// Convert AWS-specific state to generic provider state
	state := convertToProviderState(clusterName, region, vpc, cluster, nodeGroups, iamRoles)

	span.SetAttributes(
		attribute.Int("node_groups_count", len(nodeGroups)),
		attribute.Bool("vpc_discovered", vpc != nil),
		attribute.Bool("iam_roles_discovered", iamRoles != nil),
	)

	return state, nil
}

// convertToProviderState converts AWS-specific state to generic provider.InfrastructureState
func convertToProviderState(
	clusterName, region string,
	vpc *VPCState,
	cluster *ClusterState,
	nodeGroups []NodeGroupState,
	iamRoles *IAMRoles,
) *provider.InfrastructureState {
	state := &provider.InfrastructureState{
		ClusterName: clusterName,
		Provider:    "aws",
		Region:      region,
	}

	// Convert network state
	if vpc != nil {
		state.Network = &provider.NetworkState{
			ID:        vpc.VPCID,
			CIDR:      vpc.CIDR,
			SubnetIDs: append(vpc.PublicSubnetIDs, vpc.PrivateSubnetIDs...),
			Metadata: map[string]string{
				"internet_gateway_id": vpc.InternetGatewayID,
			},
		}

		// Add NAT gateway IDs to metadata
		for i, natID := range vpc.NATGatewayIDs {
			state.Network.Metadata[fmt.Sprintf("nat_gateway_%d", i)] = natID
		}

		// Add route table IDs to metadata
		state.Network.Metadata["public_route_table"] = vpc.PublicRouteTableID
		for i, rtID := range vpc.PrivateRouteTableIDs {
			state.Network.Metadata[fmt.Sprintf("private_route_table_%d", i)] = rtID
		}
	}

	// Convert cluster state
	if cluster != nil {
		state.Cluster = &provider.ClusterState{
			Name:     cluster.Name,
			ID:       cluster.ARN,
			Endpoint: cluster.Endpoint,
			Version:  cluster.Version,
			Status:   cluster.Status,
			CAData:   cluster.CertificateAuthority,
			Metadata: map[string]string{
				"oidc_provider_arn":  cluster.OIDCProviderARN,
				"endpoint_public":    fmt.Sprintf("%t", cluster.EndpointPublic),
				"endpoint_private":   fmt.Sprintf("%t", cluster.EndpointPrivate),
				"platform_version":   cluster.PlatformVersion,
				"encryption_kms_arn": cluster.EncryptionKMSKeyARN,
				"vpc_id":             cluster.VPCID,
			},
		}

		// Add enabled log types to metadata
		for i, logType := range cluster.EnabledLogTypes {
			state.Cluster.Metadata[fmt.Sprintf("enabled_log_type_%d", i)] = logType
		}

		// Add security group IDs to metadata
		for i, sgID := range cluster.SecurityGroupIDs {
			state.Cluster.Metadata[fmt.Sprintf("security_group_%d", i)] = sgID
		}

		// Add subnet IDs to metadata
		for i, subnetID := range cluster.SubnetIDs {
			state.Cluster.Metadata[fmt.Sprintf("subnet_%d", i)] = subnetID
		}

		// Add public access CIDRs to metadata
		for i, cidr := range cluster.PublicAccessCIDRs {
			state.Cluster.Metadata[fmt.Sprintf("public_access_cidr_%d", i)] = cidr
		}
	}

	// Convert node pool state
	state.NodePools = make([]provider.NodePoolState, 0, len(nodeGroups))
	for _, ng := range nodeGroups {
		// Determine if GPU enabled based on instance type or AMI type
		isGPU := false
		if ng.AMIType == "AL2_x86_64_GPU" || ng.AMIType == "AL2_ARM_64_GPU" ||
			ng.AMIType == "AL2023_x86_64_NVIDIA" || ng.AMIType == "AL2023_ARM_64_NVIDIA" ||
			ng.AMIType == "AL2023_x86_64_NEURON" ||
			ng.AMIType == "BOTTLEROCKET_x86_64_NVIDIA" || ng.AMIType == "BOTTLEROCKET_ARM_64_NVIDIA" {
			isGPU = true
		}
		// Also check instance types for GPU instances (g*, p*, inf*)
		for _, instanceType := range ng.InstanceTypes {
			if len(instanceType) > 0 {
				firstChar := instanceType[0]
				if firstChar == 'g' || firstChar == 'p' {
					isGPU = true
					break
				}
				if len(instanceType) >= 3 && instanceType[:3] == "inf" {
					isGPU = true
					break
				}
			}
		}

		// Determine if Spot enabled based on capacity type
		isSpot := ng.CapacityType == "SPOT"

		// Use first instance type as the primary instance type
		primaryInstanceType := ""
		if len(ng.InstanceTypes) > 0 {
			primaryInstanceType = ng.InstanceTypes[0]
		}

		nodePool := provider.NodePoolState{
			Name:         ng.Name,
			ID:           ng.ARN,
			InstanceType: primaryInstanceType,
			MinSize:      ng.MinSize,
			MaxSize:      ng.MaxSize,
			DesiredSize:  ng.DesiredSize,
			Status:       ng.Status,
			Labels:       ng.Labels,
			GPU:          isGPU,
			Spot:         isSpot,
			Metadata: map[string]string{
				"ami_type":      ng.AMIType,
				"capacity_type": ng.CapacityType,
				"disk_size":     fmt.Sprintf("%d", ng.DiskSize),
				"node_role_arn": ng.NodeRoleARN,
			},
		}

		// Add all instance types to metadata
		for i, instanceType := range ng.InstanceTypes {
			nodePool.Metadata[fmt.Sprintf("instance_type_%d", i)] = instanceType
		}

		// Convert taints
		nodePool.Taints = make([]provider.Taint, 0, len(ng.Taints))
		for _, taint := range ng.Taints {
			nodePool.Taints = append(nodePool.Taints, provider.Taint{
				Key:    taint.Key,
				Value:  taint.Value,
				Effect: taint.Effect,
			})
		}

		// Add subnet IDs to metadata
		for i, subnetID := range ng.SubnetIDs {
			nodePool.Metadata[fmt.Sprintf("subnet_%d", i)] = subnetID
		}

		// Add health issues to metadata if any
		for i, issue := range ng.Health.Issues {
			nodePool.Metadata[fmt.Sprintf("health_issue_%d", i)] = issue
		}

		state.NodePools = append(state.NodePools, nodePool)
	}

	// Storage state (EFS) - not yet implemented
	// Will be added when EFS support is implemented
	state.Storage = nil

	return state
}
