package aws

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// TestConvertToProviderState tests conversion to provider state
func TestConvertToProviderState(t *testing.T) {
	tests := []struct {
		name         string
		clusterName  string
		region       string
		vpc          *VPCState
		cluster      *ClusterState
		nodeGroups   []NodeGroupState
		validateFunc func(*testing.T, *provider.InfrastructureState)
	}{
		{
			name:        "minimal",
			clusterName: "test-cluster",
			region:      "us-west-2",
			cluster: &ClusterState{
				Name:                 "test-cluster",
				ARN:                  "arn:aws:eks:us-west-2:123456789012:cluster/test-cluster",
				Endpoint:             "https://ABC123.eks.us-west-2.amazonaws.com",
				Version:              "1.28",
				Status:               "ACTIVE",
				CertificateAuthority: "LS0tLS1CRUdJTi...",
			},
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if state == nil {
					t.Fatal("convertToProviderState returned nil")
				}
				if state.ClusterName != "test-cluster" {
					t.Errorf("Expected cluster name test-cluster, got %s", state.ClusterName)
				}
				if state.Provider != "aws" {
					t.Errorf("Expected provider 'aws', got %s", state.Provider)
				}
				if state.Region != "us-west-2" {
					t.Errorf("Expected region us-west-2, got %s", state.Region)
				}
				if state.Cluster == nil {
					t.Fatal("Cluster state is nil")
				}
				if state.Cluster.Name != "test-cluster" {
					t.Errorf("Expected cluster name test-cluster, got %s", state.Cluster.Name)
				}
				if state.Cluster.Version != "1.28" {
					t.Errorf("Expected version 1.28, got %s", state.Cluster.Version)
				}
				if state.Cluster.Status != "ACTIVE" {
					t.Errorf("Expected status ACTIVE, got %s", state.Cluster.Status)
				}
			},
		},
		{
			name:        "with VPC",
			clusterName: "test-cluster",
			region:      "us-west-2",
			vpc: &VPCState{
				VPCID:                "vpc-12345",
				CIDR:                 "10.0.0.0/16",
				PublicSubnetIDs:      []string{"subnet-1", "subnet-2"},
				PrivateSubnetIDs:     []string{"subnet-3", "subnet-4"},
				InternetGatewayID:    "igw-12345",
				NATGatewayIDs:        []string{"nat-1", "nat-2"},
				PublicRouteTableID:   "rtb-public",
				PrivateRouteTableIDs: []string{"rtb-1", "rtb-2"},
			},
			cluster: &ClusterState{
				Name:     "test-cluster",
				ARN:      "arn:aws:eks:us-west-2:123456789012:cluster/test-cluster",
				Endpoint: "https://ABC123.eks.us-west-2.amazonaws.com",
				Version:  "1.28",
				Status:   "ACTIVE",
			},
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if state.Network == nil {
					t.Fatal("Network state is nil")
				}
				if state.Network.ID != "vpc-12345" {
					t.Errorf("Expected VPC ID vpc-12345, got %s", state.Network.ID)
				}
				if state.Network.CIDR != "10.0.0.0/16" {
					t.Errorf("Expected CIDR 10.0.0.0/16, got %s", state.Network.CIDR)
				}
				if len(state.Network.SubnetIDs) != 4 {
					t.Errorf("Expected 4 subnets, got %d", len(state.Network.SubnetIDs))
				}
				if state.Network.Metadata["internet_gateway_id"] != "igw-12345" {
					t.Errorf("Expected IGW igw-12345, got %s", state.Network.Metadata["internet_gateway_id"])
				}
				if state.Network.Metadata["nat_gateway_0"] != "nat-1" {
					t.Errorf("Expected NAT gateway nat-1, got %s", state.Network.Metadata["nat_gateway_0"])
				}
			},
		},
		{
			name:        "with node groups",
			clusterName: "test-cluster",
			region:      "us-west-2",
			cluster: &ClusterState{
				Name:    "test-cluster",
				Version: "1.28",
				Status:  "ACTIVE",
			},
			nodeGroups: []NodeGroupState{
				{
					Name:          "default",
					ARN:           "arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/default/abc123",
					InstanceTypes: []string{"t3.medium"},
					MinSize:       1,
					MaxSize:       3,
					DesiredSize:   2,
					Status:        "ACTIVE",
					AMIType:       "AL2_x86_64",
					CapacityType:  "ON_DEMAND",
					Labels: map[string]string{
						"role": "general",
					},
					Taints: []Taint{
						{
							Key:    "dedicated",
							Value:  "compute",
							Effect: "NoSchedule",
						},
					},
				},
				{
					Name:          "gpu",
					ARN:           "arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/gpu/def456",
					InstanceTypes: []string{"g4dn.xlarge"},
					MinSize:       0,
					MaxSize:       2,
					DesiredSize:   1,
					Status:        "ACTIVE",
					AMIType:       "AL2_x86_64_GPU",
					CapacityType:  "SPOT",
				},
			},
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if len(state.NodePools) != 2 {
					t.Fatalf("Expected 2 node pools, got %d", len(state.NodePools))
				}
				// Check first node group
				np := state.NodePools[0]
				if np.Name != "default" {
					t.Errorf("Expected node pool name 'default', got %s", np.Name)
				}
				if np.InstanceType != "t3.medium" {
					t.Errorf("Expected instance type t3.medium, got %s", np.InstanceType)
				}
				if np.MinSize != 1 || np.MaxSize != 3 || np.DesiredSize != 2 {
					t.Errorf("Expected sizes (1,3,2), got (%d,%d,%d)", np.MinSize, np.MaxSize, np.DesiredSize)
				}
				if len(np.Labels) != 1 || np.Labels["role"] != "general" {
					t.Errorf("Expected label role=general, got %v", np.Labels)
				}
				if len(np.Taints) != 1 {
					t.Fatalf("Expected 1 taint, got %d", len(np.Taints))
				}
				if np.Taints[0].Key != "dedicated" || np.Taints[0].Value != "compute" || np.Taints[0].Effect != "NoSchedule" {
					t.Errorf("Taint mismatch: %+v", np.Taints[0])
				}
				if np.GPU {
					t.Error("Expected GPU=false for default node group")
				}
				// Check second node group
				gpuNP := state.NodePools[1]
				if gpuNP.Name != "gpu" {
					t.Errorf("Expected node pool name 'gpu', got %s", gpuNP.Name)
				}
				if !gpuNP.GPU {
					t.Error("Expected GPU=true for gpu node group")
				}
				if !gpuNP.Spot {
					t.Error("Expected Spot=true for gpu node group")
				}
			},
		},
		{
			name:        "nil inputs",
			clusterName: "test",
			region:      "us-west-2",
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if state == nil {
					t.Fatal("convertToProviderState returned nil")
				}
				if state.ClusterName != "test" {
					t.Errorf("Expected cluster name 'test', got %s", state.ClusterName)
				}
				if state.Network != nil {
					t.Error("Expected Network to be nil")
				}
				if state.Cluster != nil {
					t.Error("Expected Cluster to be nil")
				}
				if len(state.NodePools) != 0 {
					t.Errorf("Expected 0 node pools, got %d", len(state.NodePools))
				}
				if state.Storage != nil {
					t.Error("Expected Storage to be nil")
				}
			},
		},
		{
			name:        "cluster metadata",
			clusterName: "test",
			region:      "us-west-2",
			cluster: &ClusterState{
				Name:                "test",
				ARN:                 "arn:aws:eks:us-west-2:123:cluster/test",
				Endpoint:            "https://test.eks.amazonaws.com",
				Version:             "1.28",
				Status:              "ACTIVE",
				VPCID:               "vpc-123",
				EndpointPublic:      true,
				EndpointPrivate:     false,
				OIDCProviderARN:     "arn:aws:iam::123:oidc-provider/oidc.eks.us-west-2.amazonaws.com/id/ABC123",
				PlatformVersion:     "eks.1",
				EncryptionKMSKeyARN: "arn:aws:kms:us-west-2:123:key/abc",
				SecurityGroupIDs:    []string{"sg-1", "sg-2"},
				SubnetIDs:           []string{"subnet-1", "subnet-2"},
				PublicAccessCIDRs:   []string{"0.0.0.0/0"},
				EnabledLogTypes:     []string{"api", "audit"},
			},
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if state.Cluster == nil {
					t.Fatal("Cluster state is nil")
				}
				metadata := state.Cluster.Metadata
				if metadata["vpc_id"] != "vpc-123" {
					t.Errorf("Unexpected vpc_id: %s", metadata["vpc_id"])
				}
				if metadata["endpoint_public"] != "true" {
					t.Errorf("Expected endpoint_public=true, got %s", metadata["endpoint_public"])
				}
				if metadata["endpoint_private"] != "false" {
					t.Errorf("Expected endpoint_private=false, got %s", metadata["endpoint_private"])
				}
				if metadata["enabled_log_type_0"] != "api" {
					t.Errorf("Expected enabled_log_type_0=api, got %s", metadata["enabled_log_type_0"])
				}
				if metadata["enabled_log_type_1"] != "audit" {
					t.Errorf("Expected enabled_log_type_1=audit, got %s", metadata["enabled_log_type_1"])
				}
				if metadata["security_group_0"] != "sg-1" {
					t.Errorf("Expected security_group_0=sg-1, got %s", metadata["security_group_0"])
				}
				if metadata["subnet_0"] != "subnet-1" {
					t.Errorf("Expected subnet_0=subnet-1, got %s", metadata["subnet_0"])
				}
				if metadata["public_access_cidr_0"] != "0.0.0.0/0" {
					t.Errorf("Expected public_access_cidr_0=0.0.0.0/0, got %s", metadata["public_access_cidr_0"])
				}
			},
		},
		{
			name:        "empty node groups",
			clusterName: "test",
			region:      "us-west-2",
			cluster: &ClusterState{
				Name:    "test",
				Version: "1.28",
				Status:  "ACTIVE",
			},
			nodeGroups: []NodeGroupState{},
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if state.NodePools == nil {
					t.Fatal("NodePools should not be nil")
				}
				if len(state.NodePools) != 0 {
					t.Errorf("Expected 0 node pools, got %d", len(state.NodePools))
				}
			},
		},
		{
			name:        "infrastructure state type verification",
			clusterName: "test",
			region:      "us-west-2",
			cluster: &ClusterState{
				Name:    "test",
				Version: "1.28",
			},
			validateFunc: func(t *testing.T, state *provider.InfrastructureState) {
				if state == nil {
					t.Fatal("State should not be nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := convertToProviderState(tt.clusterName, tt.region, tt.vpc, tt.cluster, tt.nodeGroups, nil)
			if tt.validateFunc != nil {
				tt.validateFunc(t, state)
			}
		})
	}
}
