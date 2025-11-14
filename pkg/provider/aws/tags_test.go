package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestGenerateBaseTags(t *testing.T) {
	ctx := context.Background()
	clusterName := "test-cluster"
	resourceType := ResourceTypeVPC

	tags := GenerateBaseTags(ctx, clusterName, resourceType)

	// Check required tags
	if tags[TagManagedBy] != ManagedByValue {
		t.Errorf("TagManagedBy = %q, want %q", tags[TagManagedBy], ManagedByValue)
	}

	if tags[TagClusterName] != clusterName {
		t.Errorf("TagClusterName = %q, want %q", tags[TagClusterName], clusterName)
	}

	if tags[TagResourceType] != resourceType {
		t.Errorf("TagResourceType = %q, want %q", tags[TagResourceType], resourceType)
	}

	if tags[TagVersion] != NICVersion {
		t.Errorf("TagVersion = %q, want %q", tags[TagVersion], NICVersion)
	}

	// Check count
	if len(tags) != 4 {
		t.Errorf("Expected 4 base tags, got %d", len(tags))
	}
}

func TestGenerateNodePoolTags(t *testing.T) {
	ctx := context.Background()
	clusterName := "test-cluster"
	nodePoolName := "general"

	tags := GenerateNodePoolTags(ctx, clusterName, nodePoolName)

	// Check base tags
	if tags[TagManagedBy] != ManagedByValue {
		t.Errorf("TagManagedBy = %q, want %q", tags[TagManagedBy], ManagedByValue)
	}

	if tags[TagClusterName] != clusterName {
		t.Errorf("TagClusterName = %q, want %q", tags[TagClusterName], clusterName)
	}

	if tags[TagResourceType] != ResourceTypeNodePool {
		t.Errorf("TagResourceType = %q, want %q", tags[TagResourceType], ResourceTypeNodePool)
	}

	// Check node pool specific tag
	if tags[TagNodePool] != nodePoolName {
		t.Errorf("TagNodePool = %q, want %q", tags[TagNodePool], nodePoolName)
	}

	// Check count (4 base + 1 node pool)
	if len(tags) != 5 {
		t.Errorf("Expected 5 tags for node pool, got %d", len(tags))
	}
}

func TestMergeTags(t *testing.T) {
	ctx := context.Background()

	nicTags := map[string]string{
		TagManagedBy:    ManagedByValue,
		TagClusterName:  "test-cluster",
		TagResourceType: ResourceTypeVPC,
	}

	userTags := map[string]string{
		"Environment": "production",
		"Team":        "data-science",
	}

	merged := MergeTags(ctx, nicTags, userTags)

	// Check NIC tags present
	if merged[TagManagedBy] != ManagedByValue {
		t.Errorf("NIC tag not preserved in merge")
	}

	// Check user tags present
	if merged["Environment"] != "production" {
		t.Errorf("User tag not present in merge")
	}

	if merged["Team"] != "data-science" {
		t.Errorf("User tag not present in merge")
	}

	// Check total count
	expectedCount := len(nicTags) + len(userTags)
	if len(merged) != expectedCount {
		t.Errorf("Expected %d merged tags, got %d", expectedCount, len(merged))
	}
}

func TestMergeTagsNICOverride(t *testing.T) {
	ctx := context.Background()

	nicTags := map[string]string{
		TagManagedBy:   ManagedByValue,
		TagClusterName: "test-cluster",
	}

	// User tries to override NIC tag (should fail)
	userTags := map[string]string{
		TagManagedBy: "user",
		"Custom":     "value",
	}

	merged := MergeTags(ctx, nicTags, userTags)

	// NIC tag should win
	if merged[TagManagedBy] != ManagedByValue {
		t.Errorf("User was able to override NIC tag: got %q, want %q", merged[TagManagedBy], ManagedByValue)
	}

	// User's other tag should be present
	if merged["Custom"] != "value" {
		t.Errorf("User's non-conflicting tag not present")
	}
}

func TestConvertToEC2Tags(t *testing.T) {
	tags := map[string]string{
		"Key1": "Value1",
		"Key2": "Value2",
	}

	ec2Tags := ConvertToEC2Tags(tags)

	if len(ec2Tags) != 2 {
		t.Errorf("Expected 2 EC2 tags, got %d", len(ec2Tags))
	}

	// Check that tags are properly converted
	tagMap := make(map[string]string)
	for _, tag := range ec2Tags {
		if tag.Key == nil || tag.Value == nil {
			t.Error("EC2 tag has nil Key or Value")
			continue
		}
		tagMap[*tag.Key] = *tag.Value
	}

	if tagMap["Key1"] != "Value1" {
		t.Errorf("Key1 = %q, want %q", tagMap["Key1"], "Value1")
	}

	if tagMap["Key2"] != "Value2" {
		t.Errorf("Key2 = %q, want %q", tagMap["Key2"], "Value2")
	}
}

func TestConvertToEKSTags(t *testing.T) {
	tags := map[string]string{
		"Key1": "Value1",
		"Key2": "Value2",
	}

	eksTags := ConvertToEKSTags(tags)

	if len(eksTags) != 2 {
		t.Errorf("Expected 2 EKS tags, got %d", len(eksTags))
	}

	if eksTags["Key1"] != "Value1" {
		t.Errorf("Key1 = %q, want %q", eksTags["Key1"], "Value1")
	}

	if eksTags["Key2"] != "Value2" {
		t.Errorf("Key2 = %q, want %q", eksTags["Key2"], "Value2")
	}
}

func TestGenerateResourceName(t *testing.T) {
	tests := []struct {
		name         string
		clusterName  string
		resourceType string
		suffix       string
		want         string
	}{
		{
			name:         "with suffix",
			clusterName:  "my-cluster",
			resourceType: "vpc",
			suffix:       "public",
			want:         "my-cluster-vpc-public",
		},
		{
			name:         "without suffix",
			clusterName:  "my-cluster",
			resourceType: "vpc",
			suffix:       "",
			want:         "my-cluster-vpc",
		},
		{
			name:         "node pool",
			clusterName:  "prod-cluster",
			resourceType: "nodegroup",
			suffix:       "general",
			want:         "prod-cluster-nodegroup-general",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateResourceName(tt.clusterName, tt.resourceType, tt.suffix)
			if got != tt.want {
				t.Errorf("GenerateResourceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildTagFilter(t *testing.T) {
	clusterName := "test-cluster"
	resourceType := ResourceTypeVPC

	filters := BuildTagFilter(clusterName, resourceType)

	if len(filters) != 3 {
		t.Errorf("Expected 3 filters, got %d", len(filters))
	}

	// Convert to map for easier testing
	filterMap := make(map[string][]string)
	for _, filter := range filters {
		if filter.Name != nil {
			filterMap[*filter.Name] = filter.Values
		}
	}

	// Check managed-by filter
	managedByKey := "tag:" + TagManagedBy
	if values, ok := filterMap[managedByKey]; !ok || len(values) != 1 || values[0] != ManagedByValue {
		t.Errorf("Managed-by filter incorrect: %v", filterMap[managedByKey])
	}

	// Check cluster-name filter
	clusterNameKey := "tag:" + TagClusterName
	if values, ok := filterMap[clusterNameKey]; !ok || len(values) != 1 || values[0] != clusterName {
		t.Errorf("Cluster-name filter incorrect: %v", filterMap[clusterNameKey])
	}

	// Check resource-type filter
	resourceTypeKey := "tag:" + TagResourceType
	if values, ok := filterMap[resourceTypeKey]; !ok || len(values) != 1 || values[0] != resourceType {
		t.Errorf("Resource-type filter incorrect: %v", filterMap[resourceTypeKey])
	}
}

func TestTagConstants(t *testing.T) {
	// Ensure tag key constants follow the correct pattern
	expectedPrefix := "nic.nebari.dev/"

	tagKeys := []string{
		TagManagedBy,
		TagClusterName,
		TagResourceType,
		TagVersion,
		TagNodePool,
		TagEnvironment,
	}

	for _, key := range tagKeys {
		if len(key) <= len(expectedPrefix) || key[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("Tag key %q does not start with %q", key, expectedPrefix)
		}
	}

	// Check managed by value
	if ManagedByValue != "nic" {
		t.Errorf("ManagedByValue = %q, want %q", ManagedByValue, "nic")
	}

	// Check version format (should be semver-like)
	if NICVersion == "" {
		t.Error("NICVersion should not be empty")
	}
}

// Ensure types.Filter import works (compile test)
func TestFilterType(t *testing.T) {
	_ = types.Filter{}
}
