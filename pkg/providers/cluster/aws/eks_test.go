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
	DescribeClusterFunc                func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	ListPodIdentityAssociationsFunc    func(ctx context.Context, params *eks.ListPodIdentityAssociationsInput, optFns ...func(*eks.Options)) (*eks.ListPodIdentityAssociationsOutput, error)
	DescribePodIdentityAssociationFunc func(ctx context.Context, params *eks.DescribePodIdentityAssociationInput, optFns ...func(*eks.Options)) (*eks.DescribePodIdentityAssociationOutput, error)
}

func (m *mockEKSClient) DescribeCluster(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if m.DescribeClusterFunc != nil {
		return m.DescribeClusterFunc(ctx, params, optFns...)
	}
	return &eks.DescribeClusterOutput{}, nil
}

func (m *mockEKSClient) ListPodIdentityAssociations(ctx context.Context, params *eks.ListPodIdentityAssociationsInput, optFns ...func(*eks.Options)) (*eks.ListPodIdentityAssociationsOutput, error) {
	if m.ListPodIdentityAssociationsFunc != nil {
		return m.ListPodIdentityAssociationsFunc(ctx, params, optFns...)
	}
	return &eks.ListPodIdentityAssociationsOutput{}, nil
}

func (m *mockEKSClient) DescribePodIdentityAssociation(ctx context.Context, params *eks.DescribePodIdentityAssociationInput, optFns ...func(*eks.Options)) (*eks.DescribePodIdentityAssociationOutput, error) {
	if m.DescribePodIdentityAssociationFunc != nil {
		return m.DescribePodIdentityAssociationFunc(ctx, params, optFns...)
	}
	return &eks.DescribePodIdentityAssociationOutput{}, nil
}

func TestFetchBackupPodIdentityRoleARN(t *testing.T) {
	t.Run("returns role arn from the association", func(t *testing.T) {
		mock := &mockEKSClient{
			ListPodIdentityAssociationsFunc: func(_ context.Context, params *eks.ListPodIdentityAssociationsInput, _ ...func(*eks.Options)) (*eks.ListPodIdentityAssociationsOutput, error) {
				if *params.Namespace != "longhorn-system" || *params.ServiceAccount != "longhorn-service-account" {
					t.Fatalf("unexpected filter: ns=%q sa=%q", *params.Namespace, *params.ServiceAccount)
				}
				return &eks.ListPodIdentityAssociationsOutput{
					Associations: []ekstypes.PodIdentityAssociationSummary{{AssociationId: aws.String("a-123")}},
				}, nil
			},
			DescribePodIdentityAssociationFunc: func(_ context.Context, params *eks.DescribePodIdentityAssociationInput, _ ...func(*eks.Options)) (*eks.DescribePodIdentityAssociationOutput, error) {
				if *params.AssociationId != "a-123" {
					t.Fatalf("unexpected association id %q", *params.AssociationId)
				}
				return &eks.DescribePodIdentityAssociationOutput{
					Association: &ekstypes.PodIdentityAssociation{RoleArn: aws.String("arn:aws:iam::111:role/proj-longhorn-backup")},
				}, nil
			},
		}
		arn, err := fetchBackupPodIdentityRoleARN(context.Background(), mock, "proj")
		if err != nil {
			t.Fatal(err)
		}
		if arn != "arn:aws:iam::111:role/proj-longhorn-backup" {
			t.Fatalf("got %q", arn)
		}
	})

	t.Run("no association returns empty, no describe call", func(t *testing.T) {
		mock := &mockEKSClient{
			ListPodIdentityAssociationsFunc: func(_ context.Context, _ *eks.ListPodIdentityAssociationsInput, _ ...func(*eks.Options)) (*eks.ListPodIdentityAssociationsOutput, error) {
				return &eks.ListPodIdentityAssociationsOutput{}, nil
			},
			DescribePodIdentityAssociationFunc: func(_ context.Context, _ *eks.DescribePodIdentityAssociationInput, _ ...func(*eks.Options)) (*eks.DescribePodIdentityAssociationOutput, error) {
				t.Fatal("describe must not be called when no association exists")
				return nil, nil
			},
		}
		arn, err := fetchBackupPodIdentityRoleARN(context.Background(), mock, "proj")
		if err != nil || arn != "" {
			t.Fatalf("want empty/no-error, got %q err=%v", arn, err)
		}
	})

	t.Run("list error propagates", func(t *testing.T) {
		mock := &mockEKSClient{
			ListPodIdentityAssociationsFunc: func(_ context.Context, _ *eks.ListPodIdentityAssociationsInput, _ ...func(*eks.Options)) (*eks.ListPodIdentityAssociationsOutput, error) {
				return nil, errors.New("boom")
			},
		}
		if _, err := fetchBackupPodIdentityRoleARN(context.Background(), mock, "proj"); err == nil {
			t.Fatal("expected error")
		}
	})
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

// setKubeconfigCacheEntry writes an entry under the provider's lock
func setKubeconfigCacheEntry(p *Provider, key kubeconfigCacheKey, val []byte) {
	p.kubeconfigMu.Lock()
	p.kubeconfigCache[key] = val
	p.kubeconfigMu.Unlock()
}

// hasKubeconfigCacheEntry reads under the provider's read lock
func hasKubeconfigCacheEntry(p *Provider, key kubeconfigCacheKey) bool {
	p.kubeconfigMu.RLock()
	defer p.kubeconfigMu.RUnlock()
	_, ok := p.kubeconfigCache[key]
	return ok
}

func TestGetKubeconfig_ReturnsCachedValueWithoutHittingAWS(t *testing.T) {
	p := NewProvider()
	cached := []byte("preloaded-kubeconfig-bytes")
	key := kubeconfigCacheKey{projectName: "proj", region: "us-west-2"}
	setKubeconfigCacheEntry(p, key, cached)

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
	setKubeconfigCacheEntry(p, key, []byte("anything"))

	p.invalidateKubeconfigCache("proj", "us-west-2")

	if hasKubeconfigCacheEntry(p, key) {
		t.Fatalf("expected cache entry to be removed, but it is still present")
	}
}

func TestInvalidateKubeconfigCache_OnlyTouchesMatchingKey(t *testing.T) {
	p := NewProvider()
	keyFoo := kubeconfigCacheKey{projectName: "foo", region: "us-west-2"}
	keyBar := kubeconfigCacheKey{projectName: "bar", region: "us-west-2"}
	setKubeconfigCacheEntry(p, keyFoo, []byte("foo-bytes"))
	setKubeconfigCacheEntry(p, keyBar, []byte("bar-bytes"))

	p.invalidateKubeconfigCache("foo", "us-west-2")

	if hasKubeconfigCacheEntry(p, keyFoo) {
		t.Fatalf("foo entry should be gone")
	}
	if !hasKubeconfigCacheEntry(p, keyBar) {
		t.Fatalf("bar entry should still be present")
	}
}
