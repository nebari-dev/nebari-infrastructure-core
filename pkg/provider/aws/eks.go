package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// DefaultKubernetesVersion is the default Kubernetes version for EKS clusters
	DefaultKubernetesVersion = "1.28"
	// DefaultEndpointPublic is the default public endpoint access setting
	DefaultEndpointPublic = true
	// DefaultEndpointPrivate is the default private endpoint access setting
	DefaultEndpointPrivate = false

	// EKSClusterCreateTimeout is the maximum time to wait for cluster creation (10-15 minutes typical)
	EKSClusterCreateTimeout = 20 * time.Minute
	// EKSClusterUpdateTimeout is the maximum time to wait for cluster updates
	EKSClusterUpdateTimeout = 20 * time.Minute
	// EKSClusterDeleteTimeout is the maximum time to wait for cluster deletion
	EKSClusterDeleteTimeout = 15 * time.Minute
)

// createEKSCluster creates an EKS cluster with the specified configuration
func (p *Provider) createEKSCluster(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState, iamRoles *IAMRoles) (*ClusterState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createEKSCluster")
	defer span.End()

	clusterName := cfg.ProjectName

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("kubernetes_version", cfg.AmazonWebServices.KubernetesVersion),
	)

	// Get Kubernetes version (use default if not specified)
	k8sVersion := cfg.AmazonWebServices.KubernetesVersion
	if k8sVersion == "" {
		k8sVersion = DefaultKubernetesVersion
	}

	// Determine endpoint access based on EKSEndpointAccess setting
	endpointConfig := getEndpointAccessConfig(ctx, cfg.AmazonWebServices.EKSEndpointAccess)

	// Generate tags
	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeEKSCluster)
	eksTags := convertToEKSTags(nicTags)

	// Get public access CIDRs (default to all if not specified)
	publicAccessCidrs := cfg.AmazonWebServices.EKSPublicAccessCIDRs
	if len(publicAccessCidrs) == 0 {
		publicAccessCidrs = []string{"0.0.0.0/0"}
	}

	// Build VPC config - use private subnets for control plane
	vpcConfig := &ekstypes.VpcConfigRequest{
		SubnetIds:             vpc.PrivateSubnetIDs,
		EndpointPublicAccess:  aws.Bool(endpointConfig.PublicAccess),
		EndpointPrivateAccess: aws.Bool(endpointConfig.PrivateAccess),
		PublicAccessCidrs:     publicAccessCidrs,
		SecurityGroupIds:      vpc.SecurityGroupIDs,
	}

	// Build create cluster input
	createInput := &eks.CreateClusterInput{
		Name:               aws.String(clusterName),
		Version:            aws.String(k8sVersion),
		RoleArn:            aws.String(iamRoles.ClusterRoleARN),
		ResourcesVpcConfig: vpcConfig,
		Tags:               eksTags,
	}

	// Enable envelope encryption with KMS if configured
	if cfg.AmazonWebServices.EKSKMSArn != "" {
		createInput.EncryptionConfig = []ekstypes.EncryptionConfig{
			{
				Provider: &ekstypes.Provider{
					KeyArn: aws.String(cfg.AmazonWebServices.EKSKMSArn),
				},
				Resources: []string{"secrets"},
			},
		}

		span.SetAttributes(
			attribute.String("kms_key_arn", cfg.AmazonWebServices.EKSKMSArn),
		)
	}

	// Enable control plane logging
	createInput.Logging = &ekstypes.Logging{
		ClusterLogging: []ekstypes.LogSetup{
			{
				Enabled: aws.Bool(true),
				Types: []ekstypes.LogType{
					ekstypes.LogTypeApi,
					ekstypes.LogTypeAudit,
					ekstypes.LogTypeAuthenticator,
					ekstypes.LogTypeControllerManager,
					ekstypes.LogTypeScheduler,
				},
			},
		},
	}

	// Create the cluster
	createOutput, err := clients.EKSClient.CreateCluster(ctx, createInput)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create EKS cluster %s: %w", clusterName, err)
	}

	span.SetAttributes(
		attribute.String("cluster_arn", aws.ToString(createOutput.Cluster.Arn)),
		attribute.String("cluster_status", string(createOutput.Cluster.Status)),
	)

	// Wait for cluster to become active
	waiter := eks.NewClusterActiveWaiter(clients.EKSClient)
	describeInput := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	waitCtx, cancel := context.WithTimeout(ctx, EKSClusterCreateTimeout)
	defer cancel()

	describeOutput, err := waiter.WaitForOutput(waitCtx, describeInput, EKSClusterCreateTimeout)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed waiting for EKS cluster %s to become active: %w", clusterName, err)
	}

	// Convert to ClusterState
	clusterState := convertEKSClusterToState(describeOutput.Cluster)

	span.SetAttributes(
		attribute.String("final_status", string(describeOutput.Cluster.Status)),
	)

	return clusterState, nil
}

// convertEKSClusterToState converts an EKS cluster API response to ClusterState
func convertEKSClusterToState(cluster *ekstypes.Cluster) *ClusterState {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.convertEKSClusterToState")
	defer span.End()

	state := &ClusterState{
		Name:     aws.ToString(cluster.Name),
		ARN:      aws.ToString(cluster.Arn),
		Endpoint: aws.ToString(cluster.Endpoint),
		Version:  aws.ToString(cluster.Version),
		Status:   string(cluster.Status),
	}

	// Certificate authority
	if cluster.CertificateAuthority != nil {
		state.CertificateAuthority = aws.ToString(cluster.CertificateAuthority.Data)
	}

	// VPC configuration
	if cluster.ResourcesVpcConfig != nil {
		state.VPCID = aws.ToString(cluster.ResourcesVpcConfig.VpcId)
		state.SubnetIDs = cluster.ResourcesVpcConfig.SubnetIds
		state.SecurityGroupIDs = cluster.ResourcesVpcConfig.SecurityGroupIds
		state.EndpointPublic = cluster.ResourcesVpcConfig.EndpointPublicAccess
		state.EndpointPrivate = cluster.ResourcesVpcConfig.EndpointPrivateAccess
		state.PublicAccessCIDRs = cluster.ResourcesVpcConfig.PublicAccessCidrs
	}

	// OIDC provider (for IRSA - IAM Roles for Service Accounts)
	if cluster.Identity != nil && cluster.Identity.Oidc != nil {
		state.OIDCProviderARN = aws.ToString(cluster.Identity.Oidc.Issuer)
	}

	// Encryption configuration
	if len(cluster.EncryptionConfig) > 0 && cluster.EncryptionConfig[0].Provider != nil {
		state.EncryptionKMSKeyARN = aws.ToString(cluster.EncryptionConfig[0].Provider.KeyArn)
	}

	// Logging
	if cluster.Logging != nil && len(cluster.Logging.ClusterLogging) > 0 {
		for _, logSetup := range cluster.Logging.ClusterLogging {
			if aws.ToBool(logSetup.Enabled) {
				for _, logType := range logSetup.Types {
					state.EnabledLogTypes = append(state.EnabledLogTypes, string(logType))
				}
			}
		}
	}

	// Tags
	state.Tags = cluster.Tags

	// Platform version
	state.PlatformVersion = aws.ToString(cluster.PlatformVersion)

	// Created timestamp
	if cluster.CreatedAt != nil {
		state.CreatedAt = cluster.CreatedAt.Format(time.RFC3339)
	}

	span.SetAttributes(
		attribute.String("cluster_name", state.Name),
		attribute.String("cluster_status", state.Status),
	)

	return state
}

// convertToEKSTags converts NIC tags to EKS tag format (map[string]string)
func convertToEKSTags(nicTags map[string]string) map[string]string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.convertToEKSTags")
	defer span.End()

	// EKS tags are already map[string]string, so just return a copy
	tags := make(map[string]string, len(nicTags))
	for k, v := range nicTags {
		tags[k] = v
	}

	span.SetAttributes(
		attribute.Int("tag_count", len(tags)),
	)

	return tags
}
