package aws

import (
	"context"
	"errors"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// EKSClient defines the EKS operations needed to fetch cluster connection
// details and the Longhorn backup Pod Identity role.
type EKSClient interface {
	DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	ListPodIdentityAssociations(ctx context.Context, params *eks.ListPodIdentityAssociationsInput, optFns ...func(*eks.Options)) (*eks.ListPodIdentityAssociationsOutput, error)
	DescribePodIdentityAssociation(ctx context.Context, params *eks.DescribePodIdentityAssociationInput, optFns ...func(*eks.Options)) (*eks.DescribePodIdentityAssociationOutput, error)
}

func newEKSClient(ctx context.Context, region string) (EKSClient, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.newEKSClient")
	defer span.End()
	span.SetAttributes(attribute.String(attrKeyRegion, region))

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return eks.NewFromConfig(cfg), nil
}

// fetchEKSKubeconfig calls DescribeCluster and assembles a kubeconfig from
// the returned endpoint and certificate authority data. ResourceNotFound is
// translated into a friendly "run 'deploy' first" error.
func fetchEKSKubeconfig(ctx context.Context, client EKSClient, clusterName, region string) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.fetchEKSKubeconfig")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String(attrKeyRegion, region),
	)

	out, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: &clusterName,
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			err := fmt.Errorf("cluster %q not found in region %q: run 'deploy' first", clusterName, region)
			span.RecordError(err)
			return nil, err
		}
		span.RecordError(err)
		return nil, fmt.Errorf("failed to describe EKS cluster: %w", err)
	}

	if out.Cluster == nil || out.Cluster.Endpoint == nil || out.Cluster.CertificateAuthority == nil || out.Cluster.CertificateAuthority.Data == nil {
		err := fmt.Errorf("cluster %q is not ready: endpoint or CA data missing", clusterName)
		span.RecordError(err)
		return nil, err
	}

	kubeconfigBytes, err := buildKubeconfig(clusterName, *out.Cluster.Endpoint, *out.Cluster.CertificateAuthority.Data, region)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return kubeconfigBytes, nil
}

// fetchBackupPodIdentityRoleARN returns the IAM role ARN of the Pod Identity
// association bound to Longhorn's service account, or "" when none exists (the
// cluster was deployed without keyless backups). It lists associations filtered
// by namespace + service account, then describes the match to read its RoleArn
// (the list summary omits it).
func fetchBackupPodIdentityRoleARN(ctx context.Context, client EKSClient, clusterName string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.fetchBackupPodIdentityRoleARN")
	defer span.End()
	span.SetAttributes(attribute.String("cluster_name", clusterName))

	namespace := longhorn.Namespace
	serviceAccount := longhorn.ServiceAccountName
	list, err := client.ListPodIdentityAssociations(ctx, &eks.ListPodIdentityAssociationsInput{
		ClusterName:    &clusterName,
		Namespace:      &namespace,
		ServiceAccount: &serviceAccount,
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("list pod identity associations: %w", err)
	}
	if len(list.Associations) == 0 || list.Associations[0].AssociationId == nil {
		return "", nil
	}

	desc, err := client.DescribePodIdentityAssociation(ctx, &eks.DescribePodIdentityAssociationInput{
		ClusterName:   &clusterName,
		AssociationId: list.Associations[0].AssociationId,
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("describe pod identity association: %w", err)
	}
	if desc.Association == nil || desc.Association.RoleArn == nil {
		return "", nil
	}
	return *desc.Association.RoleArn, nil
}
