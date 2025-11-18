package aws

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestProviderName tests the Name method
func TestProviderName(t *testing.T) {
	provider := NewProvider()
	if provider.Name() != "aws" {
		t.Errorf("expected provider name to be 'aws', got %s", provider.Name())
	}
}

// TestNewProvider tests provider creation
func TestNewProvider(t *testing.T) {
	provider := NewProvider()
	if provider == nil {
		t.Fatal("expected provider to be non-nil")
	}
}

// TestValidate_Success tests successful validation
func TestValidate_Success(t *testing.T) {
	tests := []struct {
		name   string
		config *config.NebariConfig
	}{
		{
			name: "minimal valid config",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
		},
		{
			name: "with kubernetes version",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.28",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
		},
		{
			name: "with VPC CIDR",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:       "us-west-2",
					VPCCIDRBlock: "10.0.0.0/16",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
		},
		{
			name: "with endpoint access public",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					EKSEndpointAccess: "public",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
		},
		{
			name: "with endpoint access private",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					EKSEndpointAccess: "private",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
		},
		{
			name: "with endpoint access public-and-private",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					EKSEndpointAccess: "public-and-private",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
		},
		{
			name: "with multiple node groups",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
						"compute": {
							Instance: "c5.2xlarge",
							MinNodes: 0,
							MaxNodes: 10,
						},
						"gpu": {
							Instance: "p3.2xlarge",
							MinNodes: 0,
							MaxNodes: 5,
							GPU:      true,
						},
					},
				},
			},
		},
		{
			name: "with taints",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"gpu": {
							Instance: "p3.2xlarge",
							MinNodes: 0,
							MaxNodes: 5,
							GPU:      true,
							Taints: []config.Taint{
								{
									Key:    "nvidia.com/gpu",
									Value:  "true",
									Effect: "NoSchedule",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with all taint effects",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"special": {
							Instance: "m5.xlarge",
							MinNodes: 1,
							MaxNodes: 3,
							Taints: []config.Taint{
								{
									Key:    "workload",
									Value:  "batch",
									Effect: "NoSchedule",
								},
								{
									Key:    "priority",
									Value:  "low",
									Effect: "NoExecute",
								},
								{
									Key:    "preemptible",
									Value:  "true",
									Effect: "PreferNoSchedule",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with zero min/max nodes (defaults apply)",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 0,
							MaxNodes: 0,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider()
			ctx := context.Background()

			// Note: This will attempt to create AWS clients, which may fail
			// if credentials are not configured. We're primarily testing
			// the validation logic before the client creation.
			err := provider.Validate(ctx, tt.config)

			// If error is about AWS credentials, that's expected in test environment
			// The validation logic we care about happens before client creation
			if err != nil && !isCredentialError(err) {
				t.Errorf("Validate() error = %v, want nil (or credential error)", err)
			}
		})
	}
}

// TestValidate_MissingAWSConfig tests validation with missing AWS config
func TestValidate_MissingAWSConfig(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		// AmazonWebServices is nil
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for missing AWS configuration, got nil")
	}
	if err.Error() != "AWS configuration is required" {
		t.Errorf("expected 'AWS configuration is required', got %v", err)
	}
}

// TestValidate_MissingRegion tests validation with missing region
func TestValidate_MissingRegion(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			// Region is empty
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for missing region, got nil")
	}
	if err.Error() != "AWS region is required" {
		t.Errorf("expected 'AWS region is required', got %v", err)
	}
}

// TestValidate_InvalidKubernetesVersion tests validation with invalid K8s version
func TestValidate_InvalidKubernetesVersion(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region:            "us-west-2",
			KubernetesVersion: "1",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for invalid Kubernetes version, got nil")
	}
	expectedMsg := "invalid Kubernetes version format: 1"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_InvalidVPCCIDR tests validation with invalid VPC CIDR
func TestValidate_InvalidVPCCIDR(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region:       "us-west-2",
			VPCCIDRBlock: "10.0.0.0", // Missing /prefix
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for invalid VPC CIDR, got nil")
	}
	expectedMsg := "invalid VPC CIDR block format: 10.0.0.0 (must include /prefix)"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_InvalidEndpointAccess tests validation with invalid endpoint access
func TestValidate_InvalidEndpointAccess(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region:            "us-west-2",
			EKSEndpointAccess: "invalid",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for invalid endpoint access, got nil")
	}
	expectedMsg := "invalid EKS endpoint access: invalid (must be one of: [public private public-and-private])"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_NoNodeGroups tests validation with no node groups
func TestValidate_NoNodeGroups(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region:     "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for no node groups, got nil")
	}
	expectedMsg := "at least one node group is required"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_NodeGroupMissingInstance tests validation with missing instance type
func TestValidate_NodeGroupMissingInstance(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region: "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					// Instance is empty
					MinNodes: 1,
					MaxNodes: 3,
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for missing instance type, got nil")
	}
	expectedMsg := "node group general: instance type is required"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_NodeGroupNegativeMinNodes tests validation with negative min nodes
func TestValidate_NodeGroupNegativeMinNodes(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region: "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
					MinNodes: -1,
					MaxNodes: 3,
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for negative min nodes, got nil")
	}
	expectedMsg := "node group general: min_nodes cannot be negative"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_NodeGroupNegativeMaxNodes tests validation with negative max nodes
func TestValidate_NodeGroupNegativeMaxNodes(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region: "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
					MinNodes: 1,
					MaxNodes: -3,
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for negative max nodes, got nil")
	}
	expectedMsg := "node group general: max_nodes cannot be negative"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_NodeGroupMinGreaterThanMax tests validation when min > max
func TestValidate_NodeGroupMinGreaterThanMax(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region: "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
					MinNodes: 5,
					MaxNodes: 3,
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error when min > max, got nil")
	}
	expectedMsg := "node group general: min_nodes (5) cannot be greater than max_nodes (3)"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_TaintMissingKey tests validation with taint missing key
func TestValidate_TaintMissingKey(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region: "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{
				"gpu": {
					Instance: "p3.2xlarge",
					MinNodes: 0,
					MaxNodes: 5,
					Taints: []config.Taint{
						{
							// Key is empty
							Value:  "true",
							Effect: "NoSchedule",
						},
					},
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for taint missing key, got nil")
	}
	expectedMsg := "node group gpu: taint 0 is missing key"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// TestValidate_TaintInvalidEffect tests validation with invalid taint effect
func TestValidate_TaintInvalidEffect(t *testing.T) {
	provider := NewProvider()
	ctx := context.Background()

	config := &config.NebariConfig{
		ProjectName: "test-cluster",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region: "us-west-2",
			NodeGroups: map[string]config.AWSNodeGroup{
				"gpu": {
					Instance: "p3.2xlarge",
					MinNodes: 0,
					MaxNodes: 5,
					Taints: []config.Taint{
						{
							Key:    "nvidia.com/gpu",
							Value:  "true",
							Effect: "InvalidEffect",
						},
					},
				},
			},
		},
	}

	err := provider.Validate(ctx, config)
	if err == nil {
		t.Error("expected error for invalid taint effect, got nil")
	}
	expectedMsg := "node group gpu: taint 0 has invalid effect InvalidEffect (must be one of: [NoSchedule NoExecute PreferNoSchedule])"
	if err.Error() != expectedMsg {
		t.Errorf("expected %q, got %v", expectedMsg, err)
	}
}

// isCredentialError checks if an error is related to AWS credentials
// This helps us distinguish validation errors from credential errors in tests
func isCredentialError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsSubstring([]string{msg}, "credentials") ||
		containsSubstring([]string{msg}, "AWS_ACCESS_KEY_ID") ||
		containsSubstring([]string{msg}, "no EC2 IMDS role found") ||
		containsSubstring([]string{msg}, "failed to initialize AWS clients")
}
