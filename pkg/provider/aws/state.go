package aws

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// See https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
const maxBucketNameLength = 63

// S3Client defines the S3 operations needed for state bucket management.
type S3Client interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutPublicAccessBlock(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error)
	ListObjectVersions(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	DeleteBucket(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
}

// STSClient defines the STS operations needed to get account information.
type STSClient interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

func newS3Client(ctx context.Context, region string) (S3Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return s3.NewFromConfig(cfg), nil
}

func newSTSClient(ctx context.Context, region string) (STSClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return sts.NewFromConfig(cfg), nil
}

// generateBucketName creates a deterministic bucket name from account ID, region, and project name.
// The account ID is hashed to avoid exposing it directly in the bucket name.
func generateBucketName(accountID, region, projectName string) (string, error) {
	hash := sha256.Sum256([]byte(accountID))
	suffix := fmt.Sprintf("%x", hash[:4]) // 8 hex chars
	name := fmt.Sprintf("nic-tfstate-%s-%s-%s", projectName, region, suffix)
	if len(name) > maxBucketNameLength {
		return "", fmt.Errorf("bucket name %q exceeds %d chars: consider a shorter project name", name, maxBucketNameLength)
	}
	return name, nil
}

func stateKey(projectName string) string {
	return fmt.Sprintf("%s/terraform.tfstate", projectName)
}

// getStateBucketName generates a bucket name from the AWS account ID, region, and project name.
func getStateBucketName(ctx context.Context, client STSClient, region, projectName string) (string, error) {
	output, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get AWS account ID: %w", err)
	}
	accountID := aws.ToString(output.Account)

	return generateBucketName(accountID, region, projectName)
}

// stateBucketExists checks whether the state bucket already exists.
func stateBucketExists(ctx context.Context, client S3Client, bucketName string) (bool, error) {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		return true, nil
	}

	var notFound *types.NotFound
	var noSuchBucket *types.NoSuchBucket
	if errors.As(err, &notFound) || errors.As(err, &noSuchBucket) {
		return false, nil
	}

	return false, fmt.Errorf("failed to check if bucket exists: %w", err)
}

// ensureStateBucket creates the state bucket if it doesn't exist.
// The caller is responsible for providing the bucket name (via getStateBucketName or config override).
func ensureStateBucket(ctx context.Context, client S3Client, region, bucketName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.EnsureStateBucket")
	defer span.End()

	span.SetAttributes(
		attribute.String("bucket_name", bucketName),
		attribute.String("region", region),
	)

	// Check if bucket exists
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		span.SetAttributes(attribute.Bool("bucket_created", false))
		return nil
	}

	// If error is NotFound or NoSuchBucket, create the bucket. Other errors are returned.
	var notFound *types.NotFound
	var noSuchBucket *types.NoSuchBucket
	if !errors.As(err, &notFound) && !errors.As(err, &noSuchBucket) {
		span.RecordError(err)
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	createInput := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}
	if region != "us-east-1" {
		createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}

	if _, err := client.CreateBucket(ctx, createInput); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create state bucket: %w", err)
	}

	// Enable versioning
	_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucketName),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to enable bucket versioning: %w", err)
	}

	// Block public access
	_, err = client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(bucketName),
		PublicAccessBlockConfiguration: &types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			RestrictPublicBuckets: aws.Bool(true),
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to block public access: %w", err)
	}

	span.SetAttributes(attribute.Bool("bucket_created", true))
	return nil
}

// destroyStateBucket deletes the state bucket and all its contents.
// The caller is responsible for providing the bucket name (via getStateBucketName or config override).
func destroyStateBucket(ctx context.Context, client S3Client, region, bucketName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.DestroyStateBucket")
	defer span.End()

	span.SetAttributes(
		attribute.String("bucket_name", bucketName),
		attribute.String("region", region),
	)

	// Check if bucket exists
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var notFound *types.NotFound
		var noSuchBucket *types.NoSuchBucket
		if errors.As(err, &notFound) || errors.As(err, &noSuchBucket) {
			span.SetAttributes(attribute.Bool("bucket_existed", false))
			return nil
		}
		span.RecordError(err)
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	span.SetAttributes(attribute.Bool("bucket_existed", true))

	// Delete all object versions (required for versioned buckets)
	var objectVersions []types.ObjectIdentifier
	listVersionsPaginator := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucketName),
	})

	for listVersionsPaginator.HasMorePages() {
		page, err := listVersionsPaginator.NextPage(ctx)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to list object versions: %w", err)
		}

		for _, version := range page.Versions {
			objectVersions = append(objectVersions, types.ObjectIdentifier{
				Key:       version.Key,
				VersionId: version.VersionId,
			})
		}

		for _, deleteMarker := range page.DeleteMarkers {
			objectVersions = append(objectVersions, types.ObjectIdentifier{
				Key:       deleteMarker.Key,
				VersionId: deleteMarker.VersionId,
			})
		}
	}

	// Delete objects in batches (max 1000 per request)
	for i := 0; i < len(objectVersions); i += 1000 {
		end := i + 1000
		if end > len(objectVersions) {
			end = len(objectVersions)
		}
		batch := objectVersions[i:end]

		_, err = client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucketName),
			Delete: &types.Delete{
				Objects: batch,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete objects: %w", err)
		}
	}

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete state bucket: %w", err)
	}

	span.SetAttributes(attribute.Bool("bucket_deleted", true))
	return nil
}
