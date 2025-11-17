package aws

import (
	"context"
	"testing"
)

func TestNewClients_RequiresRegion(t *testing.T) {
	ctx := context.Background()

	// Test with empty region
	_, err := NewClients(ctx, "")
	if err == nil {
		t.Error("Expected error when region is empty, got nil")
	}

	if err != nil && err.Error() != "AWS region is required" {
		t.Errorf("Expected 'AWS region is required' error, got: %v", err)
	}
}

func TestNewClients_WithMockCredentials(t *testing.T) {
	ctx := context.Background()

	// Set mock credentials
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")

	// This should succeed with mock credentials (won't make actual AWS calls in unit tests)
	clients, err := NewClients(ctx, "us-west-2")

	if err != nil {
		t.Errorf("Expected no error with mock credentials, got: %v", err)
	}

	if clients == nil {
		t.Fatal("Expected clients to be non-nil")
	}

	// Check that clients are initialized
	if clients.EC2Client == nil {
		t.Error("EC2Client should not be nil")
	}

	if clients.EKSClient == nil {
		t.Error("EKSClient should not be nil")
	}

	if clients.IAMClient == nil {
		t.Error("IAMClient should not be nil")
	}

	if clients.EFSClient == nil {
		t.Error("EFSClient should not be nil")
	}

	if clients.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", clients.Region, "us-west-2")
	}
}

func TestNewClients_WithoutCredentials(t *testing.T) {
	ctx := context.Background()

	// Clear credentials
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	// Attempt to create clients without credentials
	// This might succeed if there are credentials in ~/.aws/credentials
	// or fail if there are no credentials available
	_, err := NewClients(ctx, "us-west-2")

	// We can't assert success or failure here because it depends on the environment
	// Just ensure the function doesn't panic
	_ = err
}

func TestNewClients_DifferentRegions(t *testing.T) {
	ctx := context.Background()

	// Set mock credentials
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")

	regions := []string{"us-west-2", "us-east-1", "eu-west-1", "ap-southeast-1"}

	for _, region := range regions {
		t.Run(region, func(t *testing.T) {
			clients, err := NewClients(ctx, region)
			if err != nil {
				t.Errorf("Failed to create clients for region %s: %v", region, err)
			}

			if clients.Region != region {
				t.Errorf("Region = %q, want %q", clients.Region, region)
			}
		})
	}
}

func TestLoadAWSConfig_WithExplicitCredentials(t *testing.T) {
	ctx := context.Background()

	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")

	cfg, err := loadAWSConfig(ctx, "us-west-2")
	if err != nil {
		t.Fatalf("loadAWSConfig failed: %v", err)
	}

	if cfg.Region != "us-west-2" {
		t.Errorf("Config region = %q, want %q", cfg.Region, "us-west-2")
	}

	// Verify credentials are available
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		t.Fatalf("Failed to retrieve credentials: %v", err)
	}

	if creds.AccessKeyID != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, "AKIAIOSFODNN7EXAMPLE")
	}
}

func TestLoadAWSConfig_WithSessionToken(t *testing.T) {
	ctx := context.Background()

	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "test-session-token")

	cfg, err := loadAWSConfig(ctx, "us-west-2")
	if err != nil {
		t.Fatalf("loadAWSConfig failed: %v", err)
	}

	// Verify credentials include session token
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		t.Fatalf("Failed to retrieve credentials: %v", err)
	}

	if creds.SessionToken != "test-session-token" {
		t.Errorf("SessionToken = %q, want %q", creds.SessionToken, "test-session-token")
	}
}
