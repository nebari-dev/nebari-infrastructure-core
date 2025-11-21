package aws

import (
	"context"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestValidate_TableDriven uses table-driven tests for all validation scenarios
func TestValidate_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.NebariConfig
		expectError bool
		errorMsg    string
	}{
		// Success cases
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
			expectError: false, // Will fail on credentials, but that's after validation logic
		},
		{
			name: "with kubernetes version",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium", MinNodes: 1, MaxNodes: 3},
					},
				},
			},
			expectError: false,
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
						"general": {Instance: "t3.medium"},
					},
				},
			},
			expectError: false,
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
						"general": {Instance: "t3.medium"},
					},
				},
			},
			expectError: false,
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
							Taints: []config.Taint{
								{Key: "workload", Value: "batch", Effect: "NoSchedule"},
								{Key: "priority", Value: "low", Effect: "NoExecute"},
								{Key: "preemptible", Value: "true", Effect: "PreferNoSchedule"},
							},
						},
					},
				},
			},
			expectError: false,
		},

		// Error cases
		{
			name: "missing AWS config",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				// AmazonWebServices is nil
			},
			expectError: true,
			errorMsg:    "AWS configuration is required",
		},
		{
			name: "missing region",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium"},
					},
				},
			},
			expectError: true,
			errorMsg:    "AWS region is required",
		},
		{
			name: "invalid kubernetes version",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid Kubernetes version format: 1",
		},
		{
			name: "invalid VPC CIDR",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:       "us-west-2",
					VPCCIDRBlock: "10.0.0.0", // Missing /prefix
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid VPC CIDR block format: 10.0.0.0 (must include /prefix)",
		},
		{
			name: "invalid endpoint access",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					EKSEndpointAccess: "invalid",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium"},
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid EKS endpoint access: invalid",
		},
		{
			name: "no node groups",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:     "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{},
				},
			},
			expectError: true,
			errorMsg:    "at least one node group is required",
		},
		{
			name: "node group missing instance",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {MinNodes: 1, MaxNodes: 3},
					},
				},
			},
			expectError: true,
			errorMsg:    "node group general: instance type is required",
		},
		{
			name: "negative min nodes",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium", MinNodes: -1},
					},
				},
			},
			expectError: true,
			errorMsg:    "node group general: min_nodes cannot be negative",
		},
		{
			name: "negative max nodes",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium", MaxNodes: -3},
					},
				},
			},
			expectError: true,
			errorMsg:    "node group general: max_nodes cannot be negative",
		},
		{
			name: "min greater than max",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {Instance: "t3.medium", MinNodes: 5, MaxNodes: 3},
					},
				},
			},
			expectError: true,
			errorMsg:    "node group general: min_nodes (5) cannot be greater than max_nodes (3)",
		},
		{
			name: "taint missing key",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"gpu": {
							Instance: "p3.2xlarge",
							Taints: []config.Taint{
								{Value: "true", Effect: "NoSchedule"},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "node group gpu: taint 0 is missing key",
		},
		{
			name: "taint invalid effect",
			config: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"gpu": {
							Instance: "p3.2xlarge",
							Taints: []config.Taint{
								{Key: "nvidia.com/gpu", Value: "true", Effect: "InvalidEffect"},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "node group gpu: taint 0 has invalid effect InvalidEffect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider()
			ctx := context.Background()

			err := provider.Validate(ctx, tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				// Success cases will fail on AWS client creation without credentials
				// That's expected and acceptable - we're testing validation logic before client creation
				if err != nil && !isCredentialError(err) {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}
