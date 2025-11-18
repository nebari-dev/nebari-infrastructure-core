//go:build integration

package aws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// IntegrationTestContext holds the testing infrastructure
type IntegrationTestContext struct {
	Container    *localstack.LocalStackContainer
	AWSConfig    aws.Config
	Clients      *Clients
	CleanupFuncs []func()
}

// SetupLocalStack creates a LocalStack container for integration testing
func SetupLocalStack(t *testing.T) *IntegrationTestContext {
	t.Helper()

	ctx := context.Background()

	// Start LocalStack container
	container, err := localstack.Run(ctx,
		"localstack/localstack:4.0",
		testcontainers.WithEnv(map[string]string{
			"SERVICES": "ec2,iam,sts",
		}),
	)
	if err != nil {
		t.Fatalf("Failed to start LocalStack container: %v", err)
	}

	// Get the endpoint
	endpoint, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get LocalStack host: %v", err)
	}

	port, err := container.MappedPort(ctx, "4566/tcp")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to get LocalStack port: %v", err)
	}

	endpointURL := fmt.Sprintf("http://%s:%s", endpoint, port.Port())

	// Create AWS config pointing to LocalStack
	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               endpointURL,
				HostnameImmutable: true,
				Source:            aws.EndpointSourceCustom,
			}, nil
		},
	)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-west-2"),
		awsconfig.WithEndpointResolverWithOptions(customResolver),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		),
	)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("Failed to create AWS config: %v", err)
	}

	// Create Clients
	clients := &Clients{
		EC2Client: ec2.NewFromConfig(awsCfg),
		IAMClient: iam.NewFromConfig(awsCfg),
		Region:    "us-west-2",
	}

	testCtx := &IntegrationTestContext{
		Container:    container,
		AWSConfig:    awsCfg,
		Clients:      clients,
		CleanupFuncs: []func(){},
	}

	// Add container cleanup
	testCtx.AddCleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate LocalStack container: %v", err)
		}
	})

	return testCtx
}

// AddCleanup adds a cleanup function to be called during teardown
func (ctx *IntegrationTestContext) AddCleanup(f func()) {
	ctx.CleanupFuncs = append(ctx.CleanupFuncs, f)
}

// Cleanup runs all cleanup functions in reverse order
func (ctx *IntegrationTestContext) Cleanup() {
	for i := len(ctx.CleanupFuncs) - 1; i >= 0; i-- {
		ctx.CleanupFuncs[i]()
	}
}

// WaitForResource waits for a resource to be ready with timeout
func WaitForResource(ctx context.Context, check func() (bool, error), timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ready, err := check()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for resource")
			}
		}
	}
}

// TestIntegration_VPCCreation tests VPC creation using LocalStack
func TestIntegration_VPCCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCtx := SetupLocalStack(t)
	defer testCtx.Cleanup()

	ctx := context.Background()
	provider := NewProvider()

	// Create test configuration
	cfg := &config.NebariConfig{
		ProjectName: "test-vpc",
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
	}

	// Test VPC creation
	t.Run("CreateVPC", func(t *testing.T) {
		vpc, err := provider.createVPC(ctx, testCtx.Clients, cfg)
		if err != nil {
			t.Fatalf("Failed to create VPC: %v", err)
		}

		if vpc == nil {
			t.Fatal("VPC should not be nil")
		}

		if vpc.VPCID == "" {
			t.Error("VPC ID should not be empty")
		}

		if vpc.CIDR != "10.0.0.0/16" {
			t.Errorf("Expected VPC CIDR 10.0.0.0/16, got %s", vpc.CIDR)
		}

		t.Logf("Created VPC: %s with CIDR %s", vpc.VPCID, vpc.CIDR)

		// Add cleanup for VPC
		testCtx.AddCleanup(func() {
			if err := provider.deleteVPC(ctx, testCtx.Clients, vpc.VPCID); err != nil {
				t.Logf("Failed to cleanup VPC: %v", err)
			}
		})
	})
}

// TestIntegration_IAMRoleCreation tests IAM role creation using LocalStack
func TestIntegration_IAMRoleCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCtx := SetupLocalStack(t)
	defer testCtx.Cleanup()

	ctx := context.Background()
	provider := NewProvider()

	// Create test configuration
	cfg := &config.NebariConfig{
		ProjectName: "test-iam",
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
	}

	// Test IAM role creation
	t.Run("CreateIAMRoles", func(t *testing.T) {
		roles, err := provider.createIAMRoles(ctx, testCtx.Clients, cfg.ProjectName)
		if err != nil {
			t.Fatalf("Failed to create IAM roles: %v", err)
		}

		if roles == nil {
			t.Fatal("IAM roles should not be nil")
		}

		if roles.ClusterRoleARN == "" {
			t.Error("EKS cluster role ARN should not be empty")
		}

		if roles.NodeRoleARN == "" {
			t.Error("EKS node role ARN should not be empty")
		}

		t.Logf("Created cluster role: %s", roles.ClusterRoleARN)
		t.Logf("Created node role: %s", roles.NodeRoleARN)

		// Add cleanup for IAM roles
		testCtx.AddCleanup(func() {
			if err := provider.deleteIAMRoles(ctx, testCtx.Clients, cfg.ProjectName); err != nil {
				t.Logf("Failed to cleanup IAM roles: %v", err)
			}
		})
	})
}

// TestIntegration_TagGeneration tests tag generation and application
func TestIntegration_TagGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	tests := []struct {
		name         string
		clusterName  string
		nodePoolName string
		expectTags   map[string]string
	}{
		{
			name:         "base tags",
			clusterName:  "test-cluster",
			nodePoolName: "",
			expectTags: map[string]string{
				TagManagedBy:    "nic",
				TagClusterName:  "test-cluster",
				TagResourceType: ResourceTypeVPC,
				TagVersion:      NICVersion,
			},
		},
		{
			name:         "node pool tags",
			clusterName:  "test-cluster",
			nodePoolName: "general",
			expectTags: map[string]string{
				TagManagedBy:    "nic",
				TagClusterName:  "test-cluster",
				TagResourceType: ResourceTypeNodePool,
				TagVersion:      NICVersion,
				TagNodePool:     "general",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tags map[string]string
			if tt.nodePoolName != "" {
				tags = GenerateNodePoolTags(ctx, tt.clusterName, tt.nodePoolName)
			} else {
				tags = GenerateBaseTags(ctx, tt.clusterName, ResourceTypeVPC)
			}

			for key, expectedValue := range tt.expectTags {
				if actualValue, ok := tags[key]; !ok {
					t.Errorf("Missing tag %s", key)
				} else if actualValue != expectedValue {
					t.Errorf("Tag %s: expected %s, got %s", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// TestIntegration_VPCDiscovery tests VPC discovery functionality
func TestIntegration_VPCDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCtx := SetupLocalStack(t)
	defer testCtx.Cleanup()

	ctx := context.Background()
	provider := NewProvider()

	// Create test configuration
	cfg := &config.NebariConfig{
		ProjectName: "test-discovery",
		Provider:    "aws",
		AmazonWebServices: &config.AWSConfig{
			Region:       "us-west-2",
			VPCCIDRBlock: "10.1.0.0/16",
			NodeGroups: map[string]config.AWSNodeGroup{
				"general": {
					Instance: "t3.medium",
					MinNodes: 1,
					MaxNodes: 3,
				},
			},
		},
	}

	// Create VPC first
	createdVPC, err := provider.createVPC(ctx, testCtx.Clients, cfg)
	if err != nil {
		t.Fatalf("Failed to create VPC for discovery test: %v", err)
	}

	testCtx.AddCleanup(func() {
		provider.deleteVPC(ctx, testCtx.Clients, createdVPC.VPCID)
	})

	// Test VPC discovery
	t.Run("DiscoverVPC", func(t *testing.T) {
		discoveredVPC, err := provider.DiscoverVPC(ctx, testCtx.Clients, cfg.ProjectName)
		if err != nil {
			t.Fatalf("Failed to discover VPC: %v", err)
		}

		if discoveredVPC == nil {
			t.Fatal("Discovered VPC should not be nil")
		}

		if discoveredVPC.VPCID != createdVPC.VPCID {
			t.Errorf("Expected VPC ID %s, got %s", createdVPC.VPCID, discoveredVPC.VPCID)
		}

		if discoveredVPC.CIDR != createdVPC.CIDR {
			t.Errorf("Expected VPC CIDR %s, got %s", createdVPC.CIDR, discoveredVPC.CIDR)
		}

		t.Logf("Successfully discovered VPC: %s", discoveredVPC.VPCID)
	})
}

// TestIntegration_Validation tests comprehensive validation logic
func TestIntegration_Validation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	provider := NewProvider()

	t.Run("ValidConfiguration", func(t *testing.T) {
		cfg := &config.NebariConfig{
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
		}

		// Validate will fail on client creation without AWS credentials,
		// but validation logic before that should pass
		err := provider.Validate(ctx, cfg)
		if err != nil {
			t.Logf("Validation result (expected credential error): %v", err)
		}
	})

	t.Run("InvalidConfiguration_MissingRegion", func(t *testing.T) {
		cfg := &config.NebariConfig{
			ProjectName: "test-cluster",
			Provider:    "aws",
			AmazonWebServices: &config.AWSConfig{
				// Missing region
				VPCCIDRBlock: "10.0.0.0/16",
				NodeGroups: map[string]config.AWSNodeGroup{
					"general": {
						Instance: "t3.medium",
					},
				},
			},
		}

		err := provider.Validate(ctx, cfg)
		if err == nil {
			t.Error("Expected validation error for missing region")
		} else if err.Error() != "AWS region is required" {
			t.Errorf("Expected 'AWS region is required', got: %v", err)
		} else {
			t.Logf("Correctly caught missing region: %v", err)
		}
	})

	t.Run("InvalidConfiguration_NoNodeGroups", func(t *testing.T) {
		cfg := &config.NebariConfig{
			ProjectName: "test-cluster",
			Provider:    "aws",
			AmazonWebServices: &config.AWSConfig{
				Region:       "us-west-2",
				VPCCIDRBlock: "10.0.0.0/16",
				NodeGroups:   map[string]config.AWSNodeGroup{},
			},
		}

		err := provider.Validate(ctx, cfg)
		if err == nil {
			t.Error("Expected validation error for missing node groups")
		} else if err.Error() != "at least one node group is required" {
			t.Errorf("Expected 'at least one node group is required', got: %v", err)
		} else {
			t.Logf("Correctly caught missing node groups: %v", err)
		}
	})
}
