package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// MockEKSClient is a mock implementation of EKSClientAPI for testing
type MockEKSClient struct {
	CreateClusterFunc         func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error)
	CreateNodegroupFunc       func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error)
	DeleteClusterFunc         func(ctx context.Context, params *eks.DeleteClusterInput, optFns ...func(*eks.Options)) (*eks.DeleteClusterOutput, error)
	DeleteNodegroupFunc       func(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error)
	DescribeClusterFunc       func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	DescribeNodegroupFunc     func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	ListNodegroupsFunc        func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error)
	UpdateClusterConfigFunc   func(ctx context.Context, params *eks.UpdateClusterConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterConfigOutput, error)
	UpdateClusterVersionFunc  func(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error)
	UpdateNodegroupConfigFunc func(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error)
}

func (m *MockEKSClient) CreateCluster(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
	if m.CreateClusterFunc != nil {
		return m.CreateClusterFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateClusterFunc not implemented")
}

func (m *MockEKSClient) CreateNodegroup(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
	if m.CreateNodegroupFunc != nil {
		return m.CreateNodegroupFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateNodegroupFunc not implemented")
}

func (m *MockEKSClient) DeleteCluster(ctx context.Context, params *eks.DeleteClusterInput, optFns ...func(*eks.Options)) (*eks.DeleteClusterOutput, error) {
	if m.DeleteClusterFunc != nil {
		return m.DeleteClusterFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteClusterFunc not implemented")
}

func (m *MockEKSClient) DeleteNodegroup(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error) {
	if m.DeleteNodegroupFunc != nil {
		return m.DeleteNodegroupFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteNodegroupFunc not implemented")
}

func (m *MockEKSClient) DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if m.DescribeClusterFunc != nil {
		return m.DescribeClusterFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeClusterFunc not implemented")
}

func (m *MockEKSClient) DescribeNodegroup(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
	if m.DescribeNodegroupFunc != nil {
		return m.DescribeNodegroupFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeNodegroupFunc not implemented")
}

func (m *MockEKSClient) ListNodegroups(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
	if m.ListNodegroupsFunc != nil {
		return m.ListNodegroupsFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("ListNodegroupsFunc not implemented")
}

func (m *MockEKSClient) UpdateClusterConfig(ctx context.Context, params *eks.UpdateClusterConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterConfigOutput, error) {
	if m.UpdateClusterConfigFunc != nil {
		return m.UpdateClusterConfigFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("UpdateClusterConfigFunc not implemented")
}

func (m *MockEKSClient) UpdateClusterVersion(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error) {
	if m.UpdateClusterVersionFunc != nil {
		return m.UpdateClusterVersionFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("UpdateClusterVersionFunc not implemented")
}

func (m *MockEKSClient) UpdateNodegroupConfig(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error) {
	if m.UpdateNodegroupConfigFunc != nil {
		return m.UpdateNodegroupConfigFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("UpdateNodegroupConfigFunc not implemented")
}

// MockEC2Client is a mock implementation of EC2ClientAPI for testing
type MockEC2Client struct {
	AllocateAddressFunc           func(ctx context.Context, params *ec2.AllocateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error)
	AssociateRouteTableFunc       func(ctx context.Context, params *ec2.AssociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.AssociateRouteTableOutput, error)
	AttachInternetGatewayFunc     func(ctx context.Context, params *ec2.AttachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.AttachInternetGatewayOutput, error)
	CreateInternetGatewayFunc     func(ctx context.Context, params *ec2.CreateInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error)
	CreateNatGatewayFunc          func(ctx context.Context, params *ec2.CreateNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateNatGatewayOutput, error)
	CreateRouteFunc               func(ctx context.Context, params *ec2.CreateRouteInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error)
	CreateRouteTableFunc          func(ctx context.Context, params *ec2.CreateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteTableOutput, error)
	CreateSecurityGroupFunc       func(ctx context.Context, params *ec2.CreateSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	CreateSubnetFunc              func(ctx context.Context, params *ec2.CreateSubnetInput, optFns ...func(*ec2.Options)) (*ec2.CreateSubnetOutput, error)
	CreateVpcFunc                 func(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error)
	DeleteInternetGatewayFunc     func(ctx context.Context, params *ec2.DeleteInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteInternetGatewayOutput, error)
	DeleteNatGatewayFunc          func(ctx context.Context, params *ec2.DeleteNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteNatGatewayOutput, error)
	DeleteRouteTableFunc          func(ctx context.Context, params *ec2.DeleteRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DeleteRouteTableOutput, error)
	DeleteSecurityGroupFunc       func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	DeleteSubnetFunc              func(ctx context.Context, params *ec2.DeleteSubnetInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSubnetOutput, error)
	DeleteVpcFunc                 func(ctx context.Context, params *ec2.DeleteVpcInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error)
	DescribeAvailabilityZonesFunc func(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error)
	DescribeInternetGatewaysFunc  func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error)
	DescribeNatGatewaysFunc       func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error)
	DescribeRouteTablesFunc       func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	DescribeSecurityGroupsFunc    func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeSubnetsFunc           func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeVpcsFunc              func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DetachInternetGatewayFunc     func(ctx context.Context, params *ec2.DetachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DetachInternetGatewayOutput, error)
	DisassociateRouteTableFunc    func(ctx context.Context, params *ec2.DisassociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DisassociateRouteTableOutput, error)
	ModifySubnetAttributeFunc     func(ctx context.Context, params *ec2.ModifySubnetAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifySubnetAttributeOutput, error)
	ModifyVpcAttributeFunc        func(ctx context.Context, params *ec2.ModifyVpcAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyVpcAttributeOutput, error)
	ReleaseAddressFunc            func(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error)
}

func (m *MockEC2Client) AllocateAddress(ctx context.Context, params *ec2.AllocateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
	if m.AllocateAddressFunc != nil {
		return m.AllocateAddressFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("AllocateAddressFunc not implemented")
}

func (m *MockEC2Client) AssociateRouteTable(ctx context.Context, params *ec2.AssociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.AssociateRouteTableOutput, error) {
	if m.AssociateRouteTableFunc != nil {
		return m.AssociateRouteTableFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("AssociateRouteTableFunc not implemented")
}

func (m *MockEC2Client) AttachInternetGateway(ctx context.Context, params *ec2.AttachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.AttachInternetGatewayOutput, error) {
	if m.AttachInternetGatewayFunc != nil {
		return m.AttachInternetGatewayFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("AttachInternetGatewayFunc not implemented")
}

func (m *MockEC2Client) CreateInternetGateway(ctx context.Context, params *ec2.CreateInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error) {
	if m.CreateInternetGatewayFunc != nil {
		return m.CreateInternetGatewayFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateInternetGatewayFunc not implemented")
}

func (m *MockEC2Client) CreateNatGateway(ctx context.Context, params *ec2.CreateNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateNatGatewayOutput, error) {
	if m.CreateNatGatewayFunc != nil {
		return m.CreateNatGatewayFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateNatGatewayFunc not implemented")
}

func (m *MockEC2Client) CreateRoute(ctx context.Context, params *ec2.CreateRouteInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error) {
	if m.CreateRouteFunc != nil {
		return m.CreateRouteFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateRouteFunc not implemented")
}

func (m *MockEC2Client) CreateRouteTable(ctx context.Context, params *ec2.CreateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteTableOutput, error) {
	if m.CreateRouteTableFunc != nil {
		return m.CreateRouteTableFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateRouteTableFunc not implemented")
}

func (m *MockEC2Client) CreateSecurityGroup(ctx context.Context, params *ec2.CreateSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
	if m.CreateSecurityGroupFunc != nil {
		return m.CreateSecurityGroupFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateSecurityGroupFunc not implemented")
}

func (m *MockEC2Client) CreateSubnet(ctx context.Context, params *ec2.CreateSubnetInput, optFns ...func(*ec2.Options)) (*ec2.CreateSubnetOutput, error) {
	if m.CreateSubnetFunc != nil {
		return m.CreateSubnetFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateSubnetFunc not implemented")
}

func (m *MockEC2Client) CreateVpc(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
	if m.CreateVpcFunc != nil {
		return m.CreateVpcFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateVpcFunc not implemented")
}

func (m *MockEC2Client) DeleteInternetGateway(ctx context.Context, params *ec2.DeleteInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteInternetGatewayOutput, error) {
	if m.DeleteInternetGatewayFunc != nil {
		return m.DeleteInternetGatewayFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteInternetGatewayFunc not implemented")
}

func (m *MockEC2Client) DeleteNatGateway(ctx context.Context, params *ec2.DeleteNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteNatGatewayOutput, error) {
	if m.DeleteNatGatewayFunc != nil {
		return m.DeleteNatGatewayFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteNatGatewayFunc not implemented")
}

func (m *MockEC2Client) DeleteRouteTable(ctx context.Context, params *ec2.DeleteRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DeleteRouteTableOutput, error) {
	if m.DeleteRouteTableFunc != nil {
		return m.DeleteRouteTableFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteRouteTableFunc not implemented")
}

func (m *MockEC2Client) DeleteSecurityGroup(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
	if m.DeleteSecurityGroupFunc != nil {
		return m.DeleteSecurityGroupFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteSecurityGroupFunc not implemented")
}

func (m *MockEC2Client) DeleteSubnet(ctx context.Context, params *ec2.DeleteSubnetInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSubnetOutput, error) {
	if m.DeleteSubnetFunc != nil {
		return m.DeleteSubnetFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteSubnetFunc not implemented")
}

func (m *MockEC2Client) DeleteVpc(ctx context.Context, params *ec2.DeleteVpcInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcOutput, error) {
	if m.DeleteVpcFunc != nil {
		return m.DeleteVpcFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteVpcFunc not implemented")
}

func (m *MockEC2Client) DescribeAvailabilityZones(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error) {
	if m.DescribeAvailabilityZonesFunc != nil {
		return m.DescribeAvailabilityZonesFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeAvailabilityZonesFunc not implemented")
}

func (m *MockEC2Client) DescribeInternetGateways(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
	if m.DescribeInternetGatewaysFunc != nil {
		return m.DescribeInternetGatewaysFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeInternetGatewaysFunc not implemented")
}

func (m *MockEC2Client) DescribeNatGateways(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
	if m.DescribeNatGatewaysFunc != nil {
		return m.DescribeNatGatewaysFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeNatGatewaysFunc not implemented")
}

func (m *MockEC2Client) DescribeRouteTables(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	if m.DescribeRouteTablesFunc != nil {
		return m.DescribeRouteTablesFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeRouteTablesFunc not implemented")
}

func (m *MockEC2Client) DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if m.DescribeSecurityGroupsFunc != nil {
		return m.DescribeSecurityGroupsFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeSecurityGroupsFunc not implemented")
}

func (m *MockEC2Client) DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if m.DescribeSubnetsFunc != nil {
		return m.DescribeSubnetsFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeSubnetsFunc not implemented")
}

func (m *MockEC2Client) DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if m.DescribeVpcsFunc != nil {
		return m.DescribeVpcsFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DescribeVpcsFunc not implemented")
}

func (m *MockEC2Client) DetachInternetGateway(ctx context.Context, params *ec2.DetachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DetachInternetGatewayOutput, error) {
	if m.DetachInternetGatewayFunc != nil {
		return m.DetachInternetGatewayFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DetachInternetGatewayFunc not implemented")
}

func (m *MockEC2Client) DisassociateRouteTable(ctx context.Context, params *ec2.DisassociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DisassociateRouteTableOutput, error) {
	if m.DisassociateRouteTableFunc != nil {
		return m.DisassociateRouteTableFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DisassociateRouteTableFunc not implemented")
}

func (m *MockEC2Client) ModifySubnetAttribute(ctx context.Context, params *ec2.ModifySubnetAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifySubnetAttributeOutput, error) {
	if m.ModifySubnetAttributeFunc != nil {
		return m.ModifySubnetAttributeFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("ModifySubnetAttributeFunc not implemented")
}

func (m *MockEC2Client) ModifyVpcAttribute(ctx context.Context, params *ec2.ModifyVpcAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyVpcAttributeOutput, error) {
	if m.ModifyVpcAttributeFunc != nil {
		return m.ModifyVpcAttributeFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("ModifyVpcAttributeFunc not implemented")
}

func (m *MockEC2Client) ReleaseAddress(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
	if m.ReleaseAddressFunc != nil {
		return m.ReleaseAddressFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("ReleaseAddressFunc not implemented")
}

// MockIAMClient is a mock implementation of IAMClientAPI for testing
type MockIAMClient struct {
	AttachRolePolicyFunc         func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	CreateRoleFunc               func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	DeleteRoleFunc               func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
	DeleteRolePolicyFunc         func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	DetachRolePolicyFunc         func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error)
	GetRoleFunc                  func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	ListAttachedRolePoliciesFunc func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
	ListRolePoliciesFunc         func(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error)
}

func (m *MockIAMClient) AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	if m.AttachRolePolicyFunc != nil {
		return m.AttachRolePolicyFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("AttachRolePolicyFunc not implemented")
}

func (m *MockIAMClient) CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	if m.CreateRoleFunc != nil {
		return m.CreateRoleFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("CreateRoleFunc not implemented")
}

func (m *MockIAMClient) DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	if m.DeleteRoleFunc != nil {
		return m.DeleteRoleFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteRoleFunc not implemented")
}

func (m *MockIAMClient) DeleteRolePolicy(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	if m.DeleteRolePolicyFunc != nil {
		return m.DeleteRolePolicyFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DeleteRolePolicyFunc not implemented")
}

func (m *MockIAMClient) DetachRolePolicy(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
	if m.DetachRolePolicyFunc != nil {
		return m.DetachRolePolicyFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("DetachRolePolicyFunc not implemented")
}

func (m *MockIAMClient) GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if m.GetRoleFunc != nil {
		return m.GetRoleFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("GetRoleFunc not implemented")
}

func (m *MockIAMClient) ListAttachedRolePolicies(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
	if m.ListAttachedRolePoliciesFunc != nil {
		return m.ListAttachedRolePoliciesFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("ListAttachedRolePoliciesFunc not implemented")
}

func (m *MockIAMClient) ListRolePolicies(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
	if m.ListRolePoliciesFunc != nil {
		return m.ListRolePoliciesFunc(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("ListRolePoliciesFunc not implemented")
}

// Compile-time verification that mocks implement the interfaces
var (
	_ EKSClientAPI = (*MockEKSClient)(nil)
	_ EC2ClientAPI = (*MockEC2Client)(nil)
	_ IAMClientAPI = (*MockIAMClient)(nil)
)
