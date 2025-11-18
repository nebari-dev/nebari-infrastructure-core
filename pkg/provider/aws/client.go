package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Clients holds all AWS service clients needed for infrastructure management
// Uses interfaces to enable mocking for unit tests
type Clients struct {
	EC2Client EC2ClientAPI
	EKSClient EKSClientAPI
	IAMClient IAMClientAPI
	EFSClient *efs.Client // Not yet interfaced - no methods currently used
	Config    aws.Config
	Region    string
}

// NewClients creates and initializes all AWS service clients
// Credentials are loaded from environment variables or AWS config files
func NewClients(ctx context.Context, region string) (*Clients, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.NewClients")
	defer span.End()

	span.SetAttributes(
		attribute.String("aws.region", region),
	)

	if region == "" {
		err := fmt.Errorf("AWS region is required")
		span.RecordError(err)
		return nil, err
	}

	// Load AWS configuration
	// This will automatically use:
	// 1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN)
	// 2. Shared config files (~/.aws/config, ~/.aws/credentials)
	// 3. EC2 instance role (if running on EC2)
	// 4. ECS task role (if running on ECS)
	cfg, err := loadAWSConfig(ctx, region)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create service clients
	clients := &Clients{
		EC2Client: ec2.NewFromConfig(cfg),
		EKSClient: eks.NewFromConfig(cfg),
		IAMClient: iam.NewFromConfig(cfg),
		EFSClient: efs.NewFromConfig(cfg),
		Config:    cfg,
		Region:    region,
	}

	span.SetAttributes(
		attribute.Bool("aws.credentials_loaded", true),
	)

	return clients, nil
}

// loadAWSConfig loads AWS configuration using the default credential chain
// The default chain checks in order:
// 1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN)
// 2. Shared credentials file (~/.aws/credentials)
// 3. Shared config file (~/.aws/config)
// 4. ECS/EC2 instance role (if running on AWS infrastructure)
func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.loadAWSConfig")
	defer span.End()

	// Create custom retry strategy with exponential backoff
	// This improves resilience for transient failures and rate limiting
	retryer := retry.NewStandard(func(so *retry.StandardOptions) {
		so.MaxAttempts = 5               // Maximum 5 attempts (1 initial + 4 retries)
		so.MaxBackoff = 30 * time.Second // Maximum backoff of 30 seconds
	})

	// Use default credential chain (env vars have highest priority)
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithRetryer(func() aws.Retryer {
			return retryer
		}),
	)
	if err != nil {
		span.RecordError(err)
		return aws.Config{}, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Validate that credentials are actually available
	_, err = cfg.Credentials.Retrieve(ctx)
	if err != nil {
		span.RecordError(err)
		return aws.Config{}, fmt.Errorf("failed to retrieve AWS credentials: %w", err)
	}

	span.SetAttributes(
		attribute.String("aws.region", region),
		attribute.Bool("aws.credentials_valid", true),
	)

	return cfg, nil
}
