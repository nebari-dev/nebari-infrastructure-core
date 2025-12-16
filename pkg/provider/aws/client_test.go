package aws

import (
	"context"
	"testing"
)

// TestNewClients tests the NewClients function with various scenarios
func TestNewClients(t *testing.T) {
	tests := []struct {
		name         string
		region       string
		setEnv       map[string]string
		expectError  bool
		errorMsg     string
		validateFunc func(*testing.T, *Clients)
	}{
		{
			name:        "requires region",
			region:      "",
			expectError: true,
			errorMsg:    "AWS region is required",
		},
		{
			name:   "with mock credentials",
			region: "us-west-2",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-key-id",
				"AWS_SECRET_ACCESS_KEY": "test-secret-key",
			},
			expectError: false,
			validateFunc: func(t *testing.T, clients *Clients) {
				if clients == nil {
					t.Fatal("Expected clients to be non-nil")
					return
				}
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
			},
		},
		{
			name:   "without credentials",
			region: "us-west-2",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "",
				"AWS_SECRET_ACCESS_KEY": "",
				"AWS_SESSION_TOKEN":     "",
			},
			// We can't assert success or failure here because it depends on the environment
			// Just ensure the function doesn't panic
		},
		{
			name:   "us-west-2 region",
			region: "us-west-2",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-key-id",
				"AWS_SECRET_ACCESS_KEY": "test-secret-key",
			},
			expectError: false,
			validateFunc: func(t *testing.T, clients *Clients) {
				if clients.Region != "us-west-2" {
					t.Errorf("Region = %q, want %q", clients.Region, "us-west-2")
				}
			},
		},
		{
			name:   "us-east-1 region",
			region: "us-east-1",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-key-id",
				"AWS_SECRET_ACCESS_KEY": "test-secret-key",
			},
			expectError: false,
			validateFunc: func(t *testing.T, clients *Clients) {
				if clients.Region != "us-east-1" {
					t.Errorf("Region = %q, want %q", clients.Region, "us-east-1")
				}
			},
		},
		{
			name:   "eu-west-1 region",
			region: "eu-west-1",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-key-id",
				"AWS_SECRET_ACCESS_KEY": "test-secret-key",
			},
			expectError: false,
			validateFunc: func(t *testing.T, clients *Clients) {
				if clients.Region != "eu-west-1" {
					t.Errorf("Region = %q, want %q", clients.Region, "eu-west-1")
				}
			},
		},
		{
			name:   "ap-southeast-1 region",
			region: "ap-southeast-1",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-key-id",
				"AWS_SECRET_ACCESS_KEY": "test-secret-key",
			},
			expectError: false,
			validateFunc: func(t *testing.T, clients *Clients) {
				if clients.Region != "ap-southeast-1" {
					t.Errorf("Region = %q, want %q", clients.Region, "ap-southeast-1")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Set environment variables
			for key, value := range tt.setEnv {
				t.Setenv(key, value)
			}

			clients, err := NewClients(ctx, tt.region)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("Expected error %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			// For non-error cases, run validation if provided
			if tt.validateFunc != nil {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}
				tt.validateFunc(t, clients)
			}
		})
	}
}

// TestLoadAWSConfig tests the loadAWSConfig function with various credential scenarios
func TestLoadAWSConfig(t *testing.T) {
	tests := []struct {
		name         string
		region       string
		setEnv       map[string]string
		expectError  bool
		validateFunc func(*testing.T, context.Context, *testing.T) // Receives cfg through closure
	}{
		{
			name:   "with explicit credentials",
			region: "us-west-2",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
				"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			expectError: false,
			validateFunc: func(t *testing.T, ctx context.Context, subT *testing.T) {
				cfg, err := loadAWSConfig(ctx, "us-west-2")
				if err != nil {
					subT.Fatalf("loadAWSConfig failed: %v", err)
				}

				if cfg.Region != "us-west-2" {
					subT.Errorf("Config region = %q, want %q", cfg.Region, "us-west-2")
				}

				// Verify credentials are available
				creds, err := cfg.Credentials.Retrieve(ctx)
				if err != nil {
					subT.Fatalf("Failed to retrieve credentials: %v", err)
				}

				if creds.AccessKeyID != "AKIAIOSFODNN7EXAMPLE" {
					subT.Errorf("AccessKeyID = %q, want %q", creds.AccessKeyID, "AKIAIOSFODNN7EXAMPLE")
				}
			},
		},
		{
			name:   "with session token",
			region: "us-west-2",
			setEnv: map[string]string{
				"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
				"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				"AWS_SESSION_TOKEN":     "test-session-token",
			},
			expectError: false,
			validateFunc: func(t *testing.T, ctx context.Context, subT *testing.T) {
				cfg, err := loadAWSConfig(ctx, "us-west-2")
				if err != nil {
					subT.Fatalf("loadAWSConfig failed: %v", err)
				}

				// Verify credentials include session token
				creds, err := cfg.Credentials.Retrieve(ctx)
				if err != nil {
					subT.Fatalf("Failed to retrieve credentials: %v", err)
				}

				if creds.SessionToken != "test-session-token" {
					subT.Errorf("SessionToken = %q, want %q", creds.SessionToken, "test-session-token")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Set environment variables
			for key, value := range tt.setEnv {
				t.Setenv(key, value)
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, ctx, t)
			}
		})
	}
}
