package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// mockEKSClient implements EKSClient for testing.
type mockEKSClient struct {
	DescribeClusterFunc func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

func (m *mockEKSClient) DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if m.DescribeClusterFunc != nil {
		return m.DescribeClusterFunc(ctx, params, optFns...)
	}
	return &eks.DescribeClusterOutput{}, nil
}

// validCAData is a base64-encoded blob accepted by buildKubeconfig.
var validCAData = base64.StdEncoding.EncodeToString([]byte("dummy-ca-bytes"))

func successOutput() *eks.DescribeClusterOutput {
	return &eks.DescribeClusterOutput{
		Cluster: &ekstypes.Cluster{
			Endpoint: aws.String("https://test.eks.amazonaws.com"),
			CertificateAuthority: &ekstypes.Certificate{
				Data: aws.String(validCAData),
			},
		},
	}
}

func TestFetchEKSKubeconfig_Success(t *testing.T) {
	mock := &mockEKSClient{
		DescribeClusterFunc: func(_ context.Context, params *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			if aws.ToString(params.Name) != "proj" {
				t.Errorf("DescribeCluster called with name=%q, want %q", aws.ToString(params.Name), "proj")
			}
			return successOutput(), nil
		},
	}

	got, err := fetchEKSKubeconfig(context.Background(), mock, "proj", "us-west-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected kubeconfig bytes, got empty output")
	}
}

func TestFetchEKSKubeconfig_ResourceNotFoundMapsToFriendlyError(t *testing.T) {
	mock := &mockEKSClient{
		DescribeClusterFunc: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			return nil, &ekstypes.ResourceNotFoundException{Message: aws.String("no such cluster")}
		},
	}

	_, err := fetchEKSKubeconfig(context.Background(), mock, "proj", "us-west-2")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "run 'deploy' first") {
		t.Fatalf("expected friendly 'run deploy first' message, got: %v", err)
	}
}

func TestFetchEKSKubeconfig_OtherAWSErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom: throttled")
	mock := &mockEKSClient{
		DescribeClusterFunc: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			return nil, sentinel
		},
	}

	_, err := fetchEKSKubeconfig(context.Background(), mock, "proj", "us-west-2")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got: %v", err)
	}
}

func TestFetchEKSKubeconfig_MissingEndpointFails(t *testing.T) {
	mock := &mockEKSClient{
		DescribeClusterFunc: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
				CertificateAuthority: &ekstypes.Certificate{Data: aws.String(validCAData)},
			}}, nil
		},
	}

	_, err := fetchEKSKubeconfig(context.Background(), mock, "proj", "us-west-2")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("expected 'not ready' error, got: %v", err)
	}
}

func TestFetchEKSKubeconfig_MissingCertificateAuthorityFails(t *testing.T) {
	mock := &mockEKSClient{
		DescribeClusterFunc: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{
				Endpoint: aws.String("https://test.eks.amazonaws.com"),
			}}, nil
		},
	}

	_, err := fetchEKSKubeconfig(context.Background(), mock, "proj", "us-west-2")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("expected 'not ready' error, got: %v", err)
	}
}

func TestGetKubeconfig_ReturnsCachedValueWithoutHittingAWS(t *testing.T) {
	p := NewProvider()
	cached := []byte("preloaded-kubeconfig-bytes")
	key := kubeconfigCacheKey{projectName: "proj", region: "us-west-2"}
	p.kubeconfigCache[key] = cached

	cc := &config.ClusterConfig{
		Providers: map[string]any{"aws": map[string]any{"region": "us-west-2"}},
	}

	got, err := p.GetKubeconfig(context.Background(), "proj", cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(cached) {
		t.Fatalf("expected cached bytes, got: %q", string(got))
	}
}

func TestInvalidateKubeconfigCache_RemovesEntry(t *testing.T) {
	p := NewProvider()
	key := kubeconfigCacheKey{projectName: "proj", region: "us-west-2"}
	p.kubeconfigCache[key] = []byte("anything")

	p.invalidateKubeconfigCache("proj", "us-west-2")

	if _, ok := p.kubeconfigCache[key]; ok {
		t.Fatalf("expected cache entry to be removed, but it is still present")
	}
}

func TestInvalidateKubeconfigCache_OnlyTouchesMatchingKey(t *testing.T) {
	p := NewProvider()
	keyFoo := kubeconfigCacheKey{projectName: "foo", region: "us-west-2"}
	keyBar := kubeconfigCacheKey{projectName: "bar", region: "us-west-2"}
	p.kubeconfigCache[keyFoo] = []byte("foo-bytes")
	p.kubeconfigCache[keyBar] = []byte("bar-bytes")

	p.invalidateKubeconfigCache("foo", "us-west-2")

	if _, ok := p.kubeconfigCache[keyFoo]; ok {
		t.Fatalf("foo entry should be gone")
	}
	if _, ok := p.kubeconfigCache[keyBar]; !ok {
		t.Fatalf("bar entry should still be present")
	}
}
