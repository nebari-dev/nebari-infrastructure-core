package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func stateBucketName(projectName string) string {
	return fmt.Sprintf("nic-tf-state-%s", projectName)
}

func stateKey(projectName string) string {
	return fmt.Sprintf("%s/terraform.tfstate", projectName)
}

func ensureStateBucket(ctx context.Context, bucketName, region string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.EnsureStateBucket")
	defer span.End()

	span.SetAttributes(
		attribute.String("bucket_name", bucketName),
		attribute.String("region", region),
	)

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)

	// Check if bucket exists
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		// Bucket exists, nothing to do
		span.SetAttributes(attribute.Bool("bucket_created", false))
		return nil
	}

	// If error is NotFound or NoSuchBucket, the bucket needs to be created. Other errors are returned.
	var notFound *types.NotFound
	var noSuchBucket *types.NoSuchBucket
	if !errors.As(err, &notFound) && !errors.As(err, &noSuchBucket) {
		span.RecordError(err)
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	createInput := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}

	// For regions other than us-east-1, we need to specify LocationConstraint
	if region != "us-east-1" {
		createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}

	_, err = client.CreateBucket(ctx, createInput)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create state bucket: %w", err)
	}

	// Enable versioning for state recovery
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

func destroyStateBucket(ctx context.Context, bucketName, region string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.DestroyStateBucket")
	defer span.End()

	span.SetAttributes(
		attribute.String("bucket_name", bucketName),
		attribute.String("region", region),
	)

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)

	// Check if bucket exists first
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var notFound *types.NotFound
		var noSuchBucket *types.NoSuchBucket
		if errors.As(err, &notFound) || errors.As(err, &noSuchBucket) {
			// Bucket doesn't exist, nothing to do
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
