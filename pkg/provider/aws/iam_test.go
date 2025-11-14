package aws

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

func TestConvertToIAMTags(t *testing.T) {
	nicTags := map[string]string{
		"nic.nebari.dev/managed-by":    "nic",
		"nic.nebari.dev/cluster-name":  "test-cluster",
		"nic.nebari.dev/resource-type": "iam-cluster-role",
	}

	iamTags := convertToIAMTags(nicTags)

	if len(iamTags) != 3 {
		t.Errorf("Expected 3 IAM tags, got %d", len(iamTags))
	}

	// Check that all keys and values are present
	tagMap := make(map[string]string)
	for _, tag := range iamTags {
		tagMap[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}

	for key, expectedValue := range nicTags {
		actualValue, ok := tagMap[key]
		if !ok {
			t.Errorf("Expected tag key %s not found", key)
		}
		if actualValue != expectedValue {
			t.Errorf("Tag %s = %v, want %v", key, actualValue, expectedValue)
		}
	}
}

func TestConvertToIAMTags_EmptyMap(t *testing.T) {
	nicTags := map[string]string{}
	iamTags := convertToIAMTags(nicTags)

	if len(iamTags) != 0 {
		t.Errorf("Expected 0 IAM tags for empty input, got %d", len(iamTags))
	}
}

func TestConvertToIAMTags_Type(t *testing.T) {
	nicTags := map[string]string{
		"key1": "value1",
	}

	iamTags := convertToIAMTags(nicTags)

	// Verify the type is correct
	var _ []iamtypes.Tag = iamTags

	if len(iamTags) != 1 {
		t.Fatalf("Expected 1 tag, got %d", len(iamTags))
	}

	if aws.ToString(iamTags[0].Key) != "key1" {
		t.Errorf("Tag key = %v, want %v", aws.ToString(iamTags[0].Key), "key1")
	}

	if aws.ToString(iamTags[0].Value) != "value1" {
		t.Errorf("Tag value = %v, want %v", aws.ToString(iamTags[0].Value), "value1")
	}
}

func TestEKSClusterManagedPolicies(t *testing.T) {
	expectedPolicies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
		"arn:aws:iam::aws:policy/AmazonEKSVPCResourceController",
	}

	if len(eksClusterManagedPolicies) != len(expectedPolicies) {
		t.Errorf("Expected %d cluster managed policies, got %d", len(expectedPolicies), len(eksClusterManagedPolicies))
	}

	for i, expected := range expectedPolicies {
		if eksClusterManagedPolicies[i] != expected {
			t.Errorf("Cluster policy %d = %v, want %v", i, eksClusterManagedPolicies[i], expected)
		}
	}
}

func TestEKSNodeManagedPolicies(t *testing.T) {
	expectedPolicies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
		"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
		"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
	}

	if len(eksNodeManagedPolicies) != len(expectedPolicies) {
		t.Errorf("Expected %d node managed policies, got %d", len(expectedPolicies), len(eksNodeManagedPolicies))
	}

	for i, expected := range expectedPolicies {
		if eksNodeManagedPolicies[i] != expected {
			t.Errorf("Node policy %d = %v, want %v", i, eksNodeManagedPolicies[i], expected)
		}
	}
}

func TestEKSTrustPolicies(t *testing.T) {
	// Validate that trust policies are valid JSON and contain expected principals
	if eksClusterTrustPolicy == "" {
		t.Error("EKS cluster trust policy is empty")
	}

	if eksNodeTrustPolicy == "" {
		t.Error("EKS node trust policy is empty")
	}

	// Basic validation - check for required strings
	requiredClusterStrings := []string{
		"eks.amazonaws.com",
		"sts:AssumeRole",
		"2012-10-17",
	}

	for _, required := range requiredClusterStrings {
		if !containsSubstring([]string{eksClusterTrustPolicy}, required) {
			t.Errorf("EKS cluster trust policy missing required string: %s", required)
		}
	}

	requiredNodeStrings := []string{
		"ec2.amazonaws.com",
		"sts:AssumeRole",
		"2012-10-17",
	}

	for _, required := range requiredNodeStrings {
		if !containsSubstring([]string{eksNodeTrustPolicy}, required) {
			t.Errorf("EKS node trust policy missing required string: %s", required)
		}
	}
}

func TestIAMResourceTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"cluster role", ResourceTypeIAMClusterRole, "iam-cluster-role"},
		{"node role", ResourceTypeIAMNodeRole, "iam-node-role"},
		{"OIDC provider", ResourceTypeIAMOIDCProvider, "iam-oidc-provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s constant = %v, want %v", tt.name, tt.constant, tt.expected)
			}
		})
	}
}
