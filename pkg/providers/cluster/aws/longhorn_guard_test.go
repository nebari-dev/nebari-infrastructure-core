package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

func TestCheckUnmanagedLonghorn(t *testing.T) {
	tests := []struct {
		name          string
		describeErr   error
		installed     bool
		installedErr  error
		wantErr       string
		wantInstalled bool // whether the release lookup must run
	}{
		{
			name:        "cluster does not exist yet",
			describeErr: &ekstypes.ResourceNotFoundException{Message: aws.String("no such cluster")},
		},
		{
			name:          "cluster without Longhorn",
			wantInstalled: true,
		},
		{
			name:          "cluster runs Longhorn",
			installed:     true,
			wantInstalled: true,
			wantErr:       "add `longhorn: {enabled: true}`",
		},
		{
			name:        "EKS API error",
			describeErr: errors.New("access denied"),
			wantErr:     "access denied",
		},
		{
			name:          "release lookup error",
			installedErr:  errors.New("connection refused"),
			wantInstalled: true,
			wantErr:       "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockEKSClient{
				DescribeClusterFunc: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					if tt.describeErr != nil {
						return nil, tt.describeErr
					}
					return successOutput(), nil
				},
			}
			called := false
			installed := func(context.Context, []byte) (bool, error) {
				called = true
				return tt.installed, tt.installedErr
			}

			err := checkUnmanagedLonghorn(context.Background(), mock, "proj", "us-west-2", installed)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("checkUnmanagedLonghorn() error = %v, want nil", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("checkUnmanagedLonghorn() error = %v, want it to contain %q", err, tt.wantErr)
			}
			if called != tt.wantInstalled {
				t.Errorf("release lookup called = %v, want %v", called, tt.wantInstalled)
			}
		})
	}
}
