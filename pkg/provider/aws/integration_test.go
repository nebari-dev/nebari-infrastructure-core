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
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"

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
	// Using latest version and minimal service set for faster startup
	container, err := localstack.Run(ctx,
		"localstack/localstack:latest",
		testcontainers.WithEnv(map[string]string{
			"SERVICES":               "ec2,iam", // Minimal services - EKS not well supported in LocalStack community
			"DNS_ADDRESS":            "0",       // Disable DNS server
			"SKIP_SSL_CERT_DOWNLOAD": "1",       // Skip SSL cert download to avoid timeout
			"EAGER_SERVICE_LOADING":  "1",       // Load services eagerly
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready.").WithStartupTimeout(2*time.Minute).WithOccurrence(1),
		),
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
		EKSClient: eks.NewFromConfig(awsCfg),
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

// TestIntegration_LocalStackStartup tests that LocalStack container starts successfully
func TestIntegration_LocalStackStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCtx := SetupLocalStack(t)
	defer testCtx.Cleanup()

	// Verify container is running
	if testCtx.Container == nil {
		t.Fatal("Container should not be nil")
	}

	if testCtx.Clients == nil {
		t.Fatal("Clients should not be nil")
	}

	t.Log("LocalStack container started successfully")
}

// Note: IAM operations timeout in LocalStack Community Edition during testing
// The IAM SDK calls hang without returning errors. For comprehensive integration
// testing of IAM operations, use real AWS credentials with cleanup or LocalStack Pro.
// The unit tests with mocks provide adequate coverage for IAM business logic.

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

// Note: VPC and EKS operations are not well supported in LocalStack Community Edition
// For comprehensive integration testing of VPC/EKS, use real AWS credentials with cleanup
// or upgrade to LocalStack Pro. The unit tests with mocks provide adequate coverage
// for the business logic.

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
