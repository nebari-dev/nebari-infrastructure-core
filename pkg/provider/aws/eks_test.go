package aws

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// TestConvertEKSClusterToState tests conversion of EKS cluster to state
func TestConvertEKSClusterToState(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		cluster      *ekstypes.Cluster
		validateFunc func(*testing.T, *ClusterState)
	}{
		{
			name: "full cluster with all fields",
			cluster: &ekstypes.Cluster{
				Name:     aws.String("test-cluster"),
				Arn:      aws.String("arn:aws:eks:us-west-2:123456789012:cluster/test-cluster"),
				Endpoint: aws.String("https://ABCDEF123456.gr7.us-west-2.eks.amazonaws.com"),
				Version:  aws.String("1.34"),
				Status:   ekstypes.ClusterStatusActive,
				CertificateAuthority: &ekstypes.Certificate{
					Data: aws.String("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t"),
				},
				ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
					VpcId:                 aws.String("vpc-123456"),
					SubnetIds:             []string{"subnet-1", "subnet-2"},
					SecurityGroupIds:      []string{"sg-123456"},
					EndpointPublicAccess:  true,
					EndpointPrivateAccess: false,
					PublicAccessCidrs:     []string{"0.0.0.0/0"},
				},
				Identity: &ekstypes.Identity{
					Oidc: &ekstypes.OIDC{
						Issuer: aws.String("https://oidc.eks.us-west-2.amazonaws.com/id/ABCDEF123456"),
					},
				},
				EncryptionConfig: []ekstypes.EncryptionConfig{
					{
						Provider: &ekstypes.Provider{
							KeyArn: aws.String("arn:aws:kms:us-west-2:123456789012:key/12345678-1234-1234-1234-123456789012"),
						},
					},
				},
				Logging: &ekstypes.Logging{
					ClusterLogging: []ekstypes.LogSetup{
						{
							Enabled: aws.Bool(true),
							Types: []ekstypes.LogType{
								ekstypes.LogTypeApi,
								ekstypes.LogTypeAudit,
							},
						},
					},
				},
				Tags: map[string]string{
					"Environment": "test",
				},
				PlatformVersion: aws.String("eks.1"),
				CreatedAt:       &now,
			},
			validateFunc: func(t *testing.T, state *ClusterState) {
				if state.Name != "test-cluster" {
					t.Errorf("Name = %v, want %v", state.Name, "test-cluster")
				}
				if state.Status != string(ekstypes.ClusterStatusActive) {
					t.Errorf("Status = %v, want %v", state.Status, ekstypes.ClusterStatusActive)
				}
				if state.Version != "1.34" {
					t.Errorf("Version = %v, want %v", state.Version, "1.34")
				}
				if state.VPCID != "vpc-123456" {
					t.Errorf("VPCID = %v, want %v", state.VPCID, "vpc-123456")
				}
				if len(state.SubnetIDs) != 2 {
					t.Errorf("SubnetIDs length = %v, want %v", len(state.SubnetIDs), 2)
				}
				if !state.EndpointPublic {
					t.Error("EndpointPublic should be true")
				}
				if state.EndpointPrivate {
					t.Error("EndpointPrivate should be false")
				}
				if state.EncryptionKMSKeyARN == "" {
					t.Error("EncryptionKMSKeyARN should not be empty")
				}
				if len(state.EnabledLogTypes) != 2 {
					t.Errorf("EnabledLogTypes length = %v, want %v", len(state.EnabledLogTypes), 2)
				}
				if state.Tags["Environment"] != "test" {
					t.Errorf("Tags[Environment] = %v, want %v", state.Tags["Environment"], "test")
				}
			},
		},
		{
			name: "minimal cluster",
			cluster: &ekstypes.Cluster{
				Name:    aws.String("minimal-cluster"),
				Arn:     aws.String("arn:aws:eks:us-west-2:123456789012:cluster/minimal-cluster"),
				Version: aws.String("1.34"),
				Status:  ekstypes.ClusterStatusCreating,
			},
			validateFunc: func(t *testing.T, state *ClusterState) {
				if state.Name != "minimal-cluster" {
					t.Errorf("Name = %v, want %v", state.Name, "minimal-cluster")
				}
				if state.Status != string(ekstypes.ClusterStatusCreating) {
					t.Errorf("Status = %v, want %v", state.Status, ekstypes.ClusterStatusCreating)
				}
				if state.Endpoint != "" {
					t.Errorf("Endpoint should be empty for minimal cluster, got %v", state.Endpoint)
				}
				if state.CertificateAuthority != "" {
					t.Errorf("CertificateAuthority should be empty for minimal cluster, got %v", state.CertificateAuthority)
				}
			},
		},
		{
			name: "logging disabled",
			cluster: &ekstypes.Cluster{
				Name:    aws.String("test-cluster"),
				Version: aws.String("1.34"),
				Status:  ekstypes.ClusterStatusActive,
				Logging: &ekstypes.Logging{
					ClusterLogging: []ekstypes.LogSetup{
						{
							Enabled: aws.Bool(false),
							Types: []ekstypes.LogType{
								ekstypes.LogTypeApi,
							},
						},
					},
				},
			},
			validateFunc: func(t *testing.T, state *ClusterState) {
				if len(state.EnabledLogTypes) != 0 {
					t.Errorf("Expected 0 enabled log types when logging is disabled, got %d", len(state.EnabledLogTypes))
				}
			},
		},
		{
			name: "nil optional values",
			cluster: &ekstypes.Cluster{
				Name:    aws.String("test-cluster"),
				Version: aws.String("1.34"),
				Status:  ekstypes.ClusterStatusActive,
				// All optional fields nil
				CertificateAuthority: nil,
				ResourcesVpcConfig:   nil,
				Identity:             nil,
				EncryptionConfig:     nil,
				Logging:              nil,
				Tags:                 nil,
				CreatedAt:            nil,
			},
			validateFunc: func(t *testing.T, state *ClusterState) {
				if state.CertificateAuthority != "" {
					t.Error("CertificateAuthority should be empty")
				}
				if state.VPCID != "" {
					t.Error("VPCID should be empty")
				}
				if len(state.SubnetIDs) != 0 {
					t.Error("SubnetIDs should be empty")
				}
				if state.OIDCProviderARN != "" {
					t.Error("OIDCProviderARN should be empty")
				}
				if state.EncryptionKMSKeyARN != "" {
					t.Error("EncryptionKMSKeyARN should be empty")
				}
				if len(state.EnabledLogTypes) != 0 {
					t.Error("EnabledLogTypes should be empty")
				}
				if state.CreatedAt != "" {
					t.Error("CreatedAt should be empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := convertEKSClusterToState(tt.cluster)
			tt.validateFunc(t, state)
		})
	}
}

// TestConvertToEKSTags tests conversion to EKS tags
func TestConvertToEKSTags(t *testing.T) {
	tests := []struct {
		name         string
		nicTags      map[string]string
		expectedLen  int
		validateFunc func(*testing.T, map[string]string, map[string]string)
	}{
		{
			name: "normal tags",
			nicTags: map[string]string{
				"nic.nebari.dev/managed-by":    "nic",
				"nic.nebari.dev/cluster-name":  "test-cluster",
				"nic.nebari.dev/resource-type": "eks-cluster",
			},
			expectedLen: 3,
			validateFunc: func(t *testing.T, nicTags, eksTags map[string]string) {
				for key, expectedValue := range nicTags {
					actualValue, ok := eksTags[key]
					if !ok {
						t.Errorf("Expected tag key %s not found", key)
					}
					if actualValue != expectedValue {
						t.Errorf("Tag %s = %v, want %v", key, actualValue, expectedValue)
					}
				}
				// Ensure it's a copy, not the same map
				nicTags["new-key"] = "new-value"
				if _, exists := eksTags["new-key"]; exists {
					t.Error("EKS tags should be a copy, not reference to original map")
				}
			},
		},
		{
			name:        "empty map",
			nicTags:     map[string]string{},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eksTags := convertToEKSTags(tt.nicTags)
			if len(eksTags) != tt.expectedLen {
				t.Errorf("Expected %d EKS tags, got %d", tt.expectedLen, len(eksTags))
			}
			if tt.validateFunc != nil {
				tt.validateFunc(t, tt.nicTags, eksTags)
			}
		})
	}
}

// TestCheckLoggingUpdate tests logging update checks
func TestCheckLoggingUpdate(t *testing.T) {
	tests := []struct {
		name        string
		state       *ClusterState
		needsUpdate bool
	}{
		{
			name: "all enabled",
			state: &ClusterState{
				EnabledLogTypes: []string{
					string(ekstypes.LogTypeApi),
					string(ekstypes.LogTypeAudit),
					string(ekstypes.LogTypeAuthenticator),
					string(ekstypes.LogTypeControllerManager),
					string(ekstypes.LogTypeScheduler),
				},
			},
			needsUpdate: false,
		},
		{
			name: "missing log types",
			state: &ClusterState{
				EnabledLogTypes: []string{
					string(ekstypes.LogTypeApi),
					string(ekstypes.LogTypeAudit),
				},
			},
			needsUpdate: true,
		},
		{
			name: "no logging",
			state: &ClusterState{
				EnabledLogTypes: []string{},
			},
			needsUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			needsUpdate := p.checkLoggingUpdate(tt.state)
			if needsUpdate != tt.needsUpdate {
				t.Errorf("checkLoggingUpdate() = %v, want %v", needsUpdate, tt.needsUpdate)
			}
		})
	}
}

// TestEKSConstants tests EKS constants
func TestEKSConstants(t *testing.T) {
	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
	}{
		{"ResourceTypeEKSCluster", ResourceTypeEKSCluster, "eks-cluster"},
		{"DefaultKubernetesVersion", DefaultKubernetesVersion, "1.34"},
		{"DefaultEndpointPublic", DefaultEndpointPublic, true},
		{"DefaultEndpointPrivate", DefaultEndpointPrivate, false},
		{"EKSClusterCreateTimeout", EKSClusterCreateTimeout, 20 * time.Minute},
		{"EKSClusterUpdateTimeout", EKSClusterUpdateTimeout, 20 * time.Minute},
		{"EKSClusterDeleteTimeout", EKSClusterDeleteTimeout, 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.actual, tt.expected)
			}
		})
	}
}
