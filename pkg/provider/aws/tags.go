package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// TagManagedBy is the NIC tag key for managed-by (all resources must have this)
	TagManagedBy = "nic.nebari.dev/managed-by"
	// TagClusterName is the NIC tag key for cluster-name (all resources must have this)
	TagClusterName = "nic.nebari.dev/cluster-name"
	// TagResourceType is the NIC tag key for resource-type (all resources must have this)
	TagResourceType = "nic.nebari.dev/resource-type"
	// TagVersion is the NIC tag key for version (all resources must have this)
	TagVersion = "nic.nebari.dev/version"

	// TagNodePool is the optional NIC tag key for node pool name
	TagNodePool = "nic.nebari.dev/node-pool"
	// TagEnvironment is the optional NIC tag key for environment
	TagEnvironment = "nic.nebari.dev/environment"

	// NICVersion is the current NIC version (updated with each release)
	NICVersion = "0.1.0"

	// ManagedByValue is the value used for the managed-by tag
	ManagedByValue = "nic"
)

// Resource type constants for tagging
const (
	ResourceTypeVPC             = "vpc"
	ResourceTypeSubnet          = "subnet"
	ResourceTypeInternetGateway = "internet-gateway"
	ResourceTypeNATGateway      = "nat-gateway"
	ResourceTypeRouteTable      = "route-table"
	ResourceTypeSecurityGroup   = "security-group"
	ResourceTypeEKSCluster      = "eks-cluster"
	ResourceTypeNodePool        = "node-pool"
	ResourceTypeEFS             = "efs"
	ResourceTypeIAMRole         = "iam-role"
	ResourceTypeEIP             = "elastic-ip"
	ResourceTypeLaunchTemplate  = "launch-template"
)

// GenerateBaseTags creates the base set of NIC tags required for all resources
func GenerateBaseTags(ctx context.Context, clusterName string, resourceType string) map[string]string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.GenerateBaseTags")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("resource_type", resourceType),
	)

	return map[string]string{
		TagManagedBy:    ManagedByValue,
		TagClusterName:  clusterName,
		TagResourceType: resourceType,
		TagVersion:      NICVersion,
	}
}

// GenerateNodePoolTags creates tags for a node pool resource
func GenerateNodePoolTags(ctx context.Context, clusterName string, nodePoolName string) map[string]string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.GenerateNodePoolTags")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("node_pool", nodePoolName),
	)

	tags := GenerateBaseTags(ctx, clusterName, ResourceTypeNodePool)
	tags[TagNodePool] = nodePoolName
	return tags
}

// MergeTags merges user-provided tags with NIC base tags
// User tags cannot override NIC tags (nic.nebari.dev/* keys)
func MergeTags(ctx context.Context, nicTags map[string]string, userTags map[string]string) map[string]string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.MergeTags")
	defer span.End()

	merged := make(map[string]string)

	// Add user tags first
	for k, v := range userTags {
		merged[k] = v
	}

	// NIC tags override user tags (especially for nic.nebari.dev/* keys)
	for k, v := range nicTags {
		merged[k] = v
	}

	span.SetAttributes(
		attribute.Int("nic_tags_count", len(nicTags)),
		attribute.Int("user_tags_count", len(userTags)),
		attribute.Int("merged_tags_count", len(merged)),
	)

	return merged
}

// ConvertToEC2Tags converts map[string]string tags to EC2 Tag type
func ConvertToEC2Tags(tags map[string]string) []types.Tag {
	ec2Tags := make([]types.Tag, 0, len(tags))
	for k, v := range tags {
		key := k
		value := v
		ec2Tags = append(ec2Tags, types.Tag{
			Key:   &key,
			Value: &value,
		})
	}
	return ec2Tags
}

// ConvertToEKSTags converts map[string]string tags to EKS tags (same format but used for consistency)
func ConvertToEKSTags(tags map[string]string) map[string]string {
	// EKS uses the same tag format as our internal representation
	eksTags := make(map[string]string, len(tags))
	for k, v := range tags {
		eksTags[k] = v
	}
	return eksTags
}

// GenerateResourceName creates a consistent resource name with cluster prefix
func GenerateResourceName(clusterName string, resourceType string, suffix string) string {
	if suffix != "" {
		return fmt.Sprintf("%s-%s-%s", clusterName, resourceType, suffix)
	}
	return fmt.Sprintf("%s-%s", clusterName, resourceType)
}

// BuildTagFilter creates EC2 API filters for discovering resources by NIC tags
func BuildTagFilter(clusterName string, resourceType string) []types.Filter {
	return []types.Filter{
		{
			Name:   stringPtr(fmt.Sprintf("tag:%s", TagManagedBy)),
			Values: []string{ManagedByValue},
		},
		{
			Name:   stringPtr(fmt.Sprintf("tag:%s", TagClusterName)),
			Values: []string{clusterName},
		},
		{
			Name:   stringPtr(fmt.Sprintf("tag:%s", TagResourceType)),
			Values: []string{resourceType},
		},
	}
}

// stringPtr returns a pointer to the provided string
func stringPtr(s string) *string {
	return &s
}
