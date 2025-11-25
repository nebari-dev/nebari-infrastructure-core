package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestDiscoverEFS(t *testing.T) {
	tests := []struct {
		name         string
		clusterName  string
		mockSetup    func(*MockEFSClient)
		expectError  bool
		expectNil    bool
		validateFunc func(*testing.T, *StorageState)
	}{
		{
			name:        "no EFS found",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEFSClient) {
				m.DescribeFileSystemsFunc = func(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
					return &efs.DescribeFileSystemsOutput{
						FileSystems: []efstypes.FileSystemDescription{},
					}, nil
				}
			},
			expectError: false,
			expectNil:   true,
		},
		{
			name:        "EFS found with matching tags",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEFSClient) {
				m.DescribeFileSystemsFunc = func(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
					return &efs.DescribeFileSystemsOutput{
						FileSystems: []efstypes.FileSystemDescription{
							{
								FileSystemId:    aws.String("fs-12345"),
								FileSystemArn:   aws.String("arn:aws:elasticfilesystem:us-west-2:123456789012:file-system/fs-12345"),
								LifeCycleState:  efstypes.LifeCycleStateAvailable,
								PerformanceMode: efstypes.PerformanceModeGeneralPurpose,
								ThroughputMode:  efstypes.ThroughputModeBursting,
								Encrypted:       aws.Bool(true),
								SizeInBytes: &efstypes.FileSystemSize{
									Value: 1024,
								},
								Tags: []efstypes.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("test-cluster")},
									{Key: aws.String(TagResourceType), Value: aws.String(ResourceTypeEFS)},
								},
							},
						},
					}, nil
				}
				m.DescribeMountTargetsFunc = func(ctx context.Context, params *efs.DescribeMountTargetsInput, optFns ...func(*efs.Options)) (*efs.DescribeMountTargetsOutput, error) {
					return &efs.DescribeMountTargetsOutput{
						MountTargets: []efstypes.MountTargetDescription{
							{
								MountTargetId:  aws.String("mt-1"),
								SubnetId:       aws.String("subnet-1"),
								IpAddress:      aws.String("10.0.1.100"),
								LifeCycleState: efstypes.LifeCycleStateAvailable,
							},
						},
					}, nil
				}
			},
			expectError: false,
			expectNil:   false,
			validateFunc: func(t *testing.T, state *StorageState) {
				if state.FileSystemID != "fs-12345" {
					t.Errorf("Expected FileSystemID 'fs-12345', got %s", state.FileSystemID)
				}
				if state.LifeCycleState != "available" {
					t.Errorf("Expected LifeCycleState 'available', got %s", state.LifeCycleState)
				}
				if state.PerformanceMode != "generalPurpose" {
					t.Errorf("Expected PerformanceMode 'generalPurpose', got %s", state.PerformanceMode)
				}
				if state.ThroughputMode != "bursting" {
					t.Errorf("Expected ThroughputMode 'bursting', got %s", state.ThroughputMode)
				}
				if !state.Encrypted {
					t.Error("Expected Encrypted to be true")
				}
				if len(state.MountTargets) != 1 {
					t.Errorf("Expected 1 mount target, got %d", len(state.MountTargets))
				}
			},
		},
		{
			name:        "EFS found without matching tags",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEFSClient) {
				m.DescribeFileSystemsFunc = func(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
					return &efs.DescribeFileSystemsOutput{
						FileSystems: []efstypes.FileSystemDescription{
							{
								FileSystemId:   aws.String("fs-other"),
								LifeCycleState: efstypes.LifeCycleStateAvailable,
								Tags: []efstypes.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("other-cluster")},
									{Key: aws.String(TagResourceType), Value: aws.String(ResourceTypeEFS)},
								},
							},
						},
					}, nil
				}
			},
			expectError: false,
			expectNil:   true,
		},
		{
			name:        "AWS API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEFSClient) {
				m.DescribeFileSystemsFunc = func(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
					return nil, fmt.Errorf("AccessDenied: access denied")
				}
			},
			expectError: true,
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEFS := &MockEFSClient{}
			tt.mockSetup(mockEFS)

			clients := &Clients{
				EFSClient: mockEFS,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			state, err := p.DiscoverEFS(ctx, clients, tt.clusterName)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.expectNil {
				if state != nil {
					t.Errorf("Expected nil state, got %+v", state)
				}
				return
			}

			if state == nil {
				t.Fatal("Expected non-nil state, got nil")
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, state)
			}
		})
	}
}

func TestReconcileEFS(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		vpc         *VPCState
		actual      *StorageState
		mockSetup   func(*MockEFSClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "EFS not enabled - no action",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS:    nil,
				},
			},
			vpc:         &VPCState{VPCID: "vpc-123"},
			actual:      nil,
			mockSetup:   func(m *MockEFSClient) {},
			expectError: false,
		},
		{
			name: "EFS enabled false - no action",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS:    &EFSConfig{Enabled: false},
				},
			},
			vpc:         &VPCState{VPCID: "vpc-123"},
			actual:      nil,
			mockSetup:   func(m *MockEFSClient) {},
			expectError: false,
		},
		{
			name: "EFS exists in non-available state - error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS:    &EFSConfig{Enabled: true},
				},
			},
			vpc: &VPCState{VPCID: "vpc-123"},
			actual: &StorageState{
				FileSystemID:   "fs-123",
				LifeCycleState: "creating",
			},
			mockSetup:   func(m *MockEFSClient) {},
			expectError: true,
			errorMsg:    "cannot reconcile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEFS := &MockEFSClient{}
			tt.mockSetup(mockEFS)

			clients := &Clients{
				EFSClient: mockEFS,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			_, err := p.reconcileEFS(ctx, clients, tt.cfg, tt.vpc, tt.actual)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !containsSubstring([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestDeleteEFS(t *testing.T) {
	tests := []struct {
		name        string
		storage     *StorageState
		mockSetup   func(*MockEFSClient)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil storage - no action",
			storage:     nil,
			mockSetup:   func(m *MockEFSClient) {},
			expectError: false,
		},
		{
			name: "successful deletion",
			storage: &StorageState{
				FileSystemID: "fs-123",
				MountTargets: []MountTarget{
					{MountTargetID: "mt-1", SubnetID: "subnet-1"},
				},
			},
			mockSetup: func(m *MockEFSClient) {
				m.DeleteMountTargetFunc = func(ctx context.Context, params *efs.DeleteMountTargetInput, optFns ...func(*efs.Options)) (*efs.DeleteMountTargetOutput, error) {
					return &efs.DeleteMountTargetOutput{}, nil
				}
				m.DescribeMountTargetsFunc = func(ctx context.Context, params *efs.DescribeMountTargetsInput, optFns ...func(*efs.Options)) (*efs.DescribeMountTargetsOutput, error) {
					return &efs.DescribeMountTargetsOutput{
						MountTargets: []efstypes.MountTargetDescription{},
					}, nil
				}
				m.DeleteFileSystemFunc = func(ctx context.Context, params *efs.DeleteFileSystemInput, optFns ...func(*efs.Options)) (*efs.DeleteFileSystemOutput, error) {
					return &efs.DeleteFileSystemOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name: "delete mount target error",
			storage: &StorageState{
				FileSystemID: "fs-123",
				MountTargets: []MountTarget{
					{MountTargetID: "mt-1", SubnetID: "subnet-1"},
				},
			},
			mockSetup: func(m *MockEFSClient) {
				m.DeleteMountTargetFunc = func(ctx context.Context, params *efs.DeleteMountTargetInput, optFns ...func(*efs.Options)) (*efs.DeleteMountTargetOutput, error) {
					return nil, fmt.Errorf("MountTargetNotFound: mount target not found")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete mount target",
		},
		{
			name: "delete file system error",
			storage: &StorageState{
				FileSystemID: "fs-123",
				MountTargets: []MountTarget{},
			},
			mockSetup: func(m *MockEFSClient) {
				m.DescribeMountTargetsFunc = func(ctx context.Context, params *efs.DescribeMountTargetsInput, optFns ...func(*efs.Options)) (*efs.DescribeMountTargetsOutput, error) {
					return &efs.DescribeMountTargetsOutput{
						MountTargets: []efstypes.MountTargetDescription{},
					}, nil
				}
				m.DeleteFileSystemFunc = func(ctx context.Context, params *efs.DeleteFileSystemInput, optFns ...func(*efs.Options)) (*efs.DeleteFileSystemOutput, error) {
					return nil, fmt.Errorf("FileSystemInUse: file system is in use")
				}
			},
			expectError: true,
			errorMsg:    "DeleteFileSystem API call failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEFS := &MockEFSClient{}
			tt.mockSetup(mockEFS)

			clients := &Clients{
				EFSClient: mockEFS,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.deleteEFS(ctx, clients, tt.storage)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !containsSubstring([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestConvertEFSTagsToMap(t *testing.T) {
	tests := []struct {
		name     string
		tags     []efstypes.Tag
		expected map[string]string
	}{
		{
			name:     "empty tags",
			tags:     []efstypes.Tag{},
			expected: map[string]string{},
		},
		{
			name: "single tag",
			tags: []efstypes.Tag{
				{Key: aws.String("key1"), Value: aws.String("value1")},
			},
			expected: map[string]string{"key1": "value1"},
		},
		{
			name: "multiple tags",
			tags: []efstypes.Tag{
				{Key: aws.String("key1"), Value: aws.String("value1")},
				{Key: aws.String("key2"), Value: aws.String("value2")},
			},
			expected: map[string]string{"key1": "value1", "key2": "value2"},
		},
		{
			name: "nil key or value skipped",
			tags: []efstypes.Tag{
				{Key: nil, Value: aws.String("value1")},
				{Key: aws.String("key2"), Value: nil},
				{Key: aws.String("key3"), Value: aws.String("value3")},
			},
			expected: map[string]string{"key3": "value3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertEFSTagsToMap(tt.tags)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d tags, got %d", len(tt.expected), len(result))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("Expected tag %s=%s, got %s", k, v, result[k])
				}
			}
		})
	}
}

func TestConvertMapToEFSTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     map[string]string
		expected int
	}{
		{
			name:     "empty map",
			tags:     map[string]string{},
			expected: 0,
		},
		{
			name:     "single tag",
			tags:     map[string]string{"key1": "value1"},
			expected: 1,
		},
		{
			name:     "multiple tags",
			tags:     map[string]string{"key1": "value1", "key2": "value2", "key3": "value3"},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMapToEFSTags(tt.tags)

			if len(result) != tt.expected {
				t.Errorf("Expected %d tags, got %d", tt.expected, len(result))
			}

			// Verify all tags are present
			resultMap := convertEFSTagsToMap(result)
			for k, v := range tt.tags {
				if resultMap[k] != v {
					t.Errorf("Expected tag %s=%s, got %s", k, v, resultMap[k])
				}
			}
		})
	}
}

// TestEFSImmutableFieldChecks tests that immutable EFS fields are properly detected
func TestEFSImmutableFieldChecks(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		vpc         *VPCState
		actual      *StorageState
		expectError bool
		errorMsg    string
	}{
		{
			name: "performance mode change attempted (immutable)",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS: &EFSConfig{
						Enabled:         true,
						PerformanceMode: "maxIO", // Different from actual
					},
				},
			},
			vpc: &VPCState{VPCID: "vpc-123"},
			actual: &StorageState{
				FileSystemID:    "fs-123",
				LifeCycleState:  "available",
				PerformanceMode: "generalPurpose", // Original
				Encrypted:       false,
			},
			expectError: true,
			errorMsg:    "performance mode is immutable",
		},
		{
			name: "encryption change attempted (immutable)",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS: &EFSConfig{
						Enabled:   true,
						Encrypted: true, // Want encrypted now
					},
				},
			},
			vpc: &VPCState{VPCID: "vpc-123"},
			actual: &StorageState{
				FileSystemID:    "fs-123",
				LifeCycleState:  "available",
				PerformanceMode: "generalPurpose",
				Encrypted:       false, // Was not encrypted
			},
			expectError: true,
			errorMsg:    "encryption setting is immutable",
		},
		{
			name: "KMS key change attempted (immutable)",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS: &EFSConfig{
						Enabled:   true,
						Encrypted: true,
						KMSKeyID:  "arn:aws:kms:us-west-2:123:key/new-key", // Different KMS key
					},
				},
			},
			vpc: &VPCState{VPCID: "vpc-123"},
			actual: &StorageState{
				FileSystemID:    "fs-123",
				LifeCycleState:  "available",
				PerformanceMode: "generalPurpose",
				Encrypted:       true,
				KMSKeyID:        "arn:aws:kms:us-west-2:123:key/original-key", // Original KMS key
			},
			expectError: true,
			errorMsg:    "KMS key is immutable",
		},
		{
			name: "all immutable fields match - no error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &Config{
					Region: "us-west-2",
					EFS: &EFSConfig{
						Enabled:         true,
						PerformanceMode: "generalPurpose",
						Encrypted:       true,
						KMSKeyID:        "arn:aws:kms:us-west-2:123:key/same-key",
						ThroughputMode:  "bursting",
					},
				},
			},
			vpc: &VPCState{
				VPCID:            "vpc-123",
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			actual: &StorageState{
				FileSystemID:    "fs-123",
				LifeCycleState:  "available",
				PerformanceMode: "generalPurpose",
				Encrypted:       true,
				KMSKeyID:        "arn:aws:kms:us-west-2:123:key/same-key",
				ThroughputMode:  "bursting",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEFS := &MockEFSClient{}
			// For success cases, we need discover calls
			if !tt.expectError {
				mockEFS.DescribeMountTargetsFunc = func(ctx context.Context, params *efs.DescribeMountTargetsInput, optFns ...func(*efs.Options)) (*efs.DescribeMountTargetsOutput, error) {
					return &efs.DescribeMountTargetsOutput{
						MountTargets: []efstypes.MountTargetDescription{
							{MountTargetId: aws.String("mt-1"), SubnetId: aws.String("subnet-1")},
						},
					}, nil
				}
				mockEFS.DescribeFileSystemsFunc = func(ctx context.Context, params *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
					return &efs.DescribeFileSystemsOutput{
						FileSystems: []efstypes.FileSystemDescription{
							{
								FileSystemId:    aws.String("fs-123"),
								FileSystemArn:   aws.String("arn:aws:efs:us-west-2:123:fs/fs-123"),
								LifeCycleState:  efstypes.LifeCycleStateAvailable,
								PerformanceMode: efstypes.PerformanceModeGeneralPurpose,
								ThroughputMode:  efstypes.ThroughputModeBursting,
								Encrypted:       aws.Bool(true),
								KmsKeyId:        aws.String("arn:aws:kms:us-west-2:123:key/same-key"),
								Tags: []efstypes.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("test-cluster")},
									{Key: aws.String(TagResourceType), Value: aws.String(ResourceTypeEFS)},
								},
							},
						},
					}, nil
				}
			}

			clients := &Clients{
				EFSClient: mockEFS,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			_, err := p.reconcileEFS(ctx, clients, tt.cfg, tt.vpc, tt.actual)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !containsSubstring([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestNeedsEFSThroughputUpdate(t *testing.T) {
	tests := []struct {
		name     string
		actual   *StorageState
		efsCfg   *EFSConfig
		expected bool
	}{
		{
			name: "no update needed - same throughput mode",
			actual: &StorageState{
				ThroughputMode: "bursting",
			},
			efsCfg: &EFSConfig{
				ThroughputMode: "bursting",
			},
			expected: false,
		},
		{
			name: "update needed - different throughput mode",
			actual: &StorageState{
				ThroughputMode: "bursting",
			},
			efsCfg: &EFSConfig{
				ThroughputMode: "elastic",
			},
			expected: true,
		},
		{
			name: "update needed - provisioned throughput change",
			actual: &StorageState{
				ThroughputMode:             "provisioned",
				ProvisionedThroughputMiBps: 100,
			},
			efsCfg: &EFSConfig{
				ThroughputMode:  "provisioned",
				ProvisionedMBps: 200,
			},
			expected: true,
		},
		{
			name: "no update - provisioned throughput same",
			actual: &StorageState{
				ThroughputMode:             "provisioned",
				ProvisionedThroughputMiBps: 100,
			},
			efsCfg: &EFSConfig{
				ThroughputMode:  "provisioned",
				ProvisionedMBps: 100,
			},
			expected: false,
		},
		{
			name: "default throughput mode used when empty",
			actual: &StorageState{
				ThroughputMode: "bursting",
			},
			efsCfg: &EFSConfig{
				ThroughputMode: "", // defaults to bursting
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsEFSThroughputUpdate(tt.actual, tt.efsCfg)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
