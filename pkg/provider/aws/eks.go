package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// EKSClient defines the EKS operations needed to fetch cluster connection details.
type EKSClient interface {
	DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

func newEKSClient(ctx context.Context, region string) (EKSClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return eks.NewFromConfig(cfg), nil
}

// fetchEKSKubeconfig calls DescribeCluster and assembles a kubeconfig from
// the returned endpoint and certificate authority data. ResourceNotFound is
// translated into a friendly "run 'deploy' first" error.
func fetchEKSKubeconfig(ctx context.Context, client EKSClient, clusterName, region string) ([]byte, error) {
	out, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: &clusterName,
	})
	if err != nil {
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("cluster %q not found in region %q: run 'deploy' first", clusterName, region)
		}
		return nil, fmt.Errorf("failed to describe EKS cluster: %w", err)
	}

	if out.Cluster == nil || out.Cluster.Endpoint == nil || out.Cluster.CertificateAuthority == nil || out.Cluster.CertificateAuthority.Data == nil {
		return nil, fmt.Errorf("cluster %q is not ready: endpoint or CA data missing", clusterName)
	}

	return buildKubeconfig(clusterName, *out.Cluster.Endpoint, *out.Cluster.CertificateAuthority.Data, region)
}
