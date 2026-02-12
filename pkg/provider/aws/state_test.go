package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// mockS3Client implements S3Client for testing.
type mockS3Client struct {
	HeadBucketFunc           func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucketFunc         func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutBucketVersioningFunc  func(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutPublicAccessBlockFunc func(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error)
	ListObjectVersionsFunc   func(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	DeleteObjectsFunc        func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	DeleteBucketFunc         func(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
}

func (m *mockS3Client) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.HeadBucketFunc != nil {
		return m.HeadBucketFunc(ctx, params, optFns...)
	}
	return &s3.HeadBucketOutput{}, nil
}

func (m *mockS3Client) CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	if m.CreateBucketFunc != nil {
		return m.CreateBucketFunc(ctx, params, optFns...)
	}
	return &s3.CreateBucketOutput{}, nil
}

func (m *mockS3Client) PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	if m.PutBucketVersioningFunc != nil {
		return m.PutBucketVersioningFunc(ctx, params, optFns...)
	}
	return &s3.PutBucketVersioningOutput{}, nil
}

func (m *mockS3Client) PutPublicAccessBlock(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	if m.PutPublicAccessBlockFunc != nil {
		return m.PutPublicAccessBlockFunc(ctx, params, optFns...)
	}
	return &s3.PutPublicAccessBlockOutput{}, nil
}

func (m *mockS3Client) ListObjectVersions(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	if m.ListObjectVersionsFunc != nil {
		return m.ListObjectVersionsFunc(ctx, params, optFns...)
	}
	return &s3.ListObjectVersionsOutput{}, nil
}

func (m *mockS3Client) DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	if m.DeleteObjectsFunc != nil {
		return m.DeleteObjectsFunc(ctx, params, optFns...)
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (m *mockS3Client) DeleteBucket(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	if m.DeleteBucketFunc != nil {
		return m.DeleteBucketFunc(ctx, params, optFns...)
	}
	return &s3.DeleteBucketOutput{}, nil
}

// mockSTSClient implements STSClient for testing.
type mockSTSClient struct {
	GetCallerIdentityFunc func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

func (m *mockSTSClient) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if m.GetCallerIdentityFunc != nil {
		return m.GetCallerIdentityFunc(ctx, params, optFns...)
	}
	return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")}, nil
}

func TestGenerateBucketName(t *testing.T) {
	t.Run("generates deterministic name with hashed account ID", func(t *testing.T) {
		name1, err := generateBucketName("123456789012", "us-east-1", "myproject")
		if err != nil {
			t.Fatalf("generateBucketName() error = %v", err)
		}

		name2, err := generateBucketName("123456789012", "us-east-1", "myproject")
		if err != nil {
			t.Fatalf("generateBucketName() error = %v", err)
		}

		if name1 != name2 {
			t.Errorf("generateBucketName() not deterministic: %q != %q", name1, name2)
		}

		if !strings.Contains(name1, "myproject") {
			t.Errorf("generateBucketName() = %q, should contain project name", name1)
		}
		if !strings.Contains(name1, "us-east-1") {
			t.Errorf("generateBucketName() = %q, should contain region", name1)
		}
	})

	t.Run("different accounts produce different names", func(t *testing.T) {
		name1, _ := generateBucketName("123456789012", "us-east-1", "myproject")
		name2, _ := generateBucketName("999999999999", "us-east-1", "myproject")

		if name1 == name2 {
			t.Errorf("different accounts should produce different names: %q == %q", name1, name2)
		}
	})

	t.Run("different regions produce different names", func(t *testing.T) {
		name1, _ := generateBucketName("123456789012", "us-east-1", "myproject")
		name2, _ := generateBucketName("123456789012", "eu-west-1", "myproject")

		if name1 == name2 {
			t.Errorf("different regions should produce different names: %q == %q", name1, name2)
		}
	})

	t.Run("returns error for name exceeding max length", func(t *testing.T) {
		longProjectName := "this-is-a-very-long-project-name-that-will-exceed-the-limit"
		_, err := generateBucketName("123456789012", "us-east-1", longProjectName)
		if err == nil {
			t.Error("generateBucketName() expected error for long name, got nil")
		}
	})
}

func TestStateKey(t *testing.T) {
	tests := []struct {
		projectName string
		want        string
	}{
		{"myproject", "myproject/terraform.tfstate"},
		{"test-123", "test-123/terraform.tfstate"},
	}
	for _, tt := range tests {
		if got := stateKey(tt.projectName); got != tt.want {
			t.Errorf("stateKey(%q) = %q, want %q", tt.projectName, got, tt.want)
		}
	}
}

func TestGetStateBucketName(t *testing.T) {
	t.Run("generates name from account ID", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")}, nil
			},
		}

		name, err := getStateBucketName(context.Background(), stsMock, "us-east-1", "myproject")
		if err != nil {
			t.Errorf("getStateBucketName() error = %v, want nil", err)
		}
		if name == "" {
			t.Error("getStateBucketName() returned empty name")
		}
		if !strings.Contains(name, "myproject") {
			t.Errorf("getStateBucketName() = %q, should contain project name", name)
		}
	})

	t.Run("returns error when STS fails", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return nil, errors.New("STS error")
			},
		}

		_, err := getStateBucketName(context.Background(), stsMock, "us-east-1", "myproject")
		if err == nil {
			t.Error("getStateBucketName() expected error, got nil")
		}
	})
}

func TestStateBucketExists(t *testing.T) {
	t.Run("returns true when bucket exists", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			},
		}

		exists, err := stateBucketExists(context.Background(), s3MockClient, "my-bucket")
		if err != nil {
			t.Errorf("stateBucketExists() error = %v, want nil", err)
		}
		if !exists {
			t.Error("stateBucketExists() = false, want true")
		}
	})

	t.Run("returns false when bucket not found", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &types.NotFound{}
			},
		}

		exists, err := stateBucketExists(context.Background(), s3MockClient, "my-bucket")
		if err != nil {
			t.Errorf("stateBucketExists() error = %v, want nil", err)
		}
		if exists {
			t.Error("stateBucketExists() = true, want false")
		}
	})

	t.Run("returns false when NoSuchBucket", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &types.NoSuchBucket{}
			},
		}

		exists, err := stateBucketExists(context.Background(), s3MockClient, "my-bucket")
		if err != nil {
			t.Errorf("stateBucketExists() error = %v, want nil", err)
		}
		if exists {
			t.Error("stateBucketExists() = true, want false")
		}
	})

	t.Run("returns error on unexpected failure", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, errors.New("access denied")
			},
		}

		_, err := stateBucketExists(context.Background(), s3MockClient, "my-bucket")
		if err == nil {
			t.Error("stateBucketExists() expected error, got nil")
		}
	})
}

func TestEnsureStateBucket(t *testing.T) {
	t.Run("bucket already exists", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			},
		}

		err := ensureStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err != nil {
			t.Errorf("ensureStateBucket() error = %v, want nil", err)
		}
	})

	t.Run("creates bucket when not found", func(t *testing.T) {
		createCalled := false
		versioningCalled := false
		publicAccessCalled := false

		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &types.NotFound{}
			},
			CreateBucketFunc: func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
				createCalled = true
				return &s3.CreateBucketOutput{}, nil
			},
			PutBucketVersioningFunc: func(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
				versioningCalled = true
				if params.VersioningConfiguration.Status != types.BucketVersioningStatusEnabled {
					t.Error("PutBucketVersioning should enable versioning")
				}
				return &s3.PutBucketVersioningOutput{}, nil
			},
			PutPublicAccessBlockFunc: func(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
				publicAccessCalled = true
				cfg := params.PublicAccessBlockConfiguration
				if !aws.ToBool(cfg.BlockPublicAcls) || !aws.ToBool(cfg.BlockPublicPolicy) ||
					!aws.ToBool(cfg.IgnorePublicAcls) || !aws.ToBool(cfg.RestrictPublicBuckets) {
					t.Error("PutPublicAccessBlock should block all public access")
				}
				return &s3.PutPublicAccessBlockOutput{}, nil
			},
		}

		err := ensureStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err != nil {
			t.Errorf("ensureStateBucket() error = %v, want nil", err)
		}
		if !createCalled {
			t.Error("CreateBucket was not called")
		}
		if !versioningCalled {
			t.Error("PutBucketVersioning was not called")
		}
		if !publicAccessCalled {
			t.Error("PutPublicAccessBlock was not called")
		}
	})

	t.Run("uses provided bucket name", func(t *testing.T) {
		var capturedBucket string
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				capturedBucket = aws.ToString(params.Bucket)
				return &s3.HeadBucketOutput{}, nil
			},
		}

		err := ensureStateBucket(context.Background(), s3MockClient, "us-east-1", "custom-bucket")
		if err != nil {
			t.Errorf("ensureStateBucket() error = %v, want nil", err)
		}
		if capturedBucket != "custom-bucket" {
			t.Errorf("HeadBucket called with bucket = %q, want %q", capturedBucket, "custom-bucket")
		}
	})

	t.Run("sets location constraint for non-us-east-1 regions", func(t *testing.T) {
		var locationConstraint types.BucketLocationConstraint
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &types.NotFound{}
			},
			CreateBucketFunc: func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
				if params.CreateBucketConfiguration != nil {
					locationConstraint = params.CreateBucketConfiguration.LocationConstraint
				}
				return &s3.CreateBucketOutput{}, nil
			},
		}

		err := ensureStateBucket(context.Background(), s3MockClient, "eu-west-1", "my-bucket")
		if err != nil {
			t.Errorf("ensureStateBucket() error = %v, want nil", err)
		}
		if locationConstraint != types.BucketLocationConstraint("eu-west-1") {
			t.Errorf("LocationConstraint = %q, want %q", locationConstraint, "eu-west-1")
		}
	})

	t.Run("returns error on CreateBucket failure", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &types.NotFound{}
			},
			CreateBucketFunc: func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
				return nil, errors.New("bucket creation failed")
			},
		}

		err := ensureStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err == nil {
			t.Error("ensureStateBucket() expected error, got nil")
		}
	})
}

func TestDestroyStateBucket(t *testing.T) {
	t.Run("bucket does not exist", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &types.NotFound{}
			},
		}

		err := destroyStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err != nil {
			t.Errorf("destroyStateBucket() error = %v, want nil", err)
		}
	})

	t.Run("deletes empty bucket", func(t *testing.T) {
		deleteBucketCalled := false

		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			},
			ListObjectVersionsFunc: func(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
				return &s3.ListObjectVersionsOutput{
					IsTruncated: aws.Bool(false),
				}, nil
			},
			DeleteBucketFunc: func(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
				deleteBucketCalled = true
				return &s3.DeleteBucketOutput{}, nil
			},
		}

		err := destroyStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err != nil {
			t.Errorf("destroyStateBucket() error = %v, want nil", err)
		}
		if !deleteBucketCalled {
			t.Error("DeleteBucket was not called")
		}
	})

	t.Run("deletes objects before bucket", func(t *testing.T) {
		deleteObjectsCalled := false

		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			},
			ListObjectVersionsFunc: func(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
				return &s3.ListObjectVersionsOutput{
					Versions: []types.ObjectVersion{
						{Key: aws.String("terraform.tfstate"), VersionId: aws.String("v1")},
						{Key: aws.String("terraform.tfstate"), VersionId: aws.String("v2")},
					},
					DeleteMarkers: []types.DeleteMarkerEntry{
						{Key: aws.String("terraform.tfstate"), VersionId: aws.String("dm1")},
					},
					IsTruncated: aws.Bool(false),
				}, nil
			},
			DeleteObjectsFunc: func(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
				deleteObjectsCalled = true
				if len(params.Delete.Objects) != 3 {
					t.Errorf("DeleteObjects objects count = %d, want 3", len(params.Delete.Objects))
				}
				return &s3.DeleteObjectsOutput{}, nil
			},
			DeleteBucketFunc: func(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
				return &s3.DeleteBucketOutput{}, nil
			},
		}

		err := destroyStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err != nil {
			t.Errorf("destroyStateBucket() error = %v, want nil", err)
		}
		if !deleteObjectsCalled {
			t.Error("DeleteObjects was not called")
		}
	})

	t.Run("returns error on DeleteBucket failure", func(t *testing.T) {
		s3MockClient := &mockS3Client{
			HeadBucketFunc: func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			},
			ListObjectVersionsFunc: func(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
				return &s3.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}, nil
			},
			DeleteBucketFunc: func(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
				return nil, errors.New("deletion failed")
			},
		}

		err := destroyStateBucket(context.Background(), s3MockClient, "us-east-1", "my-bucket")
		if err == nil {
			t.Error("destroyStateBucket() expected error, got nil")
		}
	})
}
