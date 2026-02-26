//go:build integration

package aws

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegration_LocalStack_S3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tempDir := t.TempDir()

	// Generate unique bucket name
	bucketName := fmt.Sprintf("integration-test-bucket-%d", time.Now().Unix())

	tfConfig := fmt.Sprintf(`
terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

provider "aws" {
  region                      = "us-east-2"
  access_key                  = "test"
  secret_key                  = "test"

  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
  skip_region_validation      = true

  s3_use_path_style           = true

  endpoints {
    s3 = "http://localhost:4566"
  }
}

resource "aws_s3_bucket" "test" {
  bucket = "%s"
}
`, bucketName)

	mainFile := filepath.Join(tempDir, "main.tf")

	if err := os.WriteFile(mainFile, []byte(tfConfig), 0644); err != nil {
		t.Fatalf("failed to write terraform config: %v", err)
	}

	run := func(args ...string) {
		cmd := exec.Command("terraform", args...)
		cmd.Dir = tempDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("terraform %v failed: %v", args, err)
		}
	}

	run("init", "-input=false")
	run("apply", "-auto-approve", "-input=false")
	run("destroy", "-auto-approve", "-input=false")
}