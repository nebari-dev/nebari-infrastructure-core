package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// AWSClients holds all AWS service clients needed for infrastructure management
type AWSClients struct {
	EC2Client *ec2.Client
	EKSClient *eks.Client
	IAMClient *iam.Client
	EFSClient *efs.Client
	Config    aws.Config
	Region    string
}

// NewAWSClients creates and initializes all AWS service clients
// Credentials are loaded from environment variables or AWS config files
func NewAWSClients(ctx context.Context, region string) (*AWSClients, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.NewAWSClients")
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
	clients := &AWSClients{
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

// loadAWSConfig loads AWS configuration with credentials from environment or config files
func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.loadAWSConfig")
	defer span.End()

	// Check for explicit credentials in environment variables
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessionToken := os.Getenv("AWS_SESSION_TOKEN")

	var cfg aws.Config
	var err error

	if accessKeyID != "" && secretAccessKey != "" {
		// Use explicit credentials from environment
		span.SetAttributes(attribute.String("aws.credential_source", "environment"))

		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				accessKeyID,
				secretAccessKey,
				sessionToken,
			)),
		)
	} else {
		// Use default credential chain (config files, instance role, etc.)
		span.SetAttributes(attribute.String("aws.credential_source", "default_chain"))

		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
		)
	}

	if err != nil {
		span.RecordError(err)
		return aws.Config{}, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Validate that credentials are actually available
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		span.RecordError(err)
		return aws.Config{}, fmt.Errorf("failed to retrieve AWS credentials: %w", err)
	}

	if creds.AccessKeyID == "" {
		err := fmt.Errorf("AWS credentials not found. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables")
		span.RecordError(err)
		return aws.Config{}, err
	}

	span.SetAttributes(
		attribute.String("aws.region", region),
		attribute.Bool("aws.credentials_valid", true),
	)

	return cfg, nil
}
