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

func TestUninstallLonghornBeforeDestroy(t *testing.T) {
	tests := []struct {
		name          string
		kubeconfigErr error
		uninstallErr  error
		force         bool
		wantCalled    bool
		wantErr       bool
		wantForced    bool
	}{
		{name: "uninstalls", wantCalled: true},
		{name: "kubeconfig unavailable skips", kubeconfigErr: errors.New("cluster gone")},
		{name: "failure aborts without force", uninstallErr: errors.New("hook failed"), wantCalled: true, wantErr: true},
		{name: "failure continues with force", uninstallErr: errors.New("hook failed"), force: true, wantCalled: true, wantForced: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			uninstall := func(context.Context, []byte) error {
				called = true
				return tt.uninstallErr
			}

			forced, err := uninstallLonghornBeforeDestroy(context.Background(), []byte("kubeconfig"), tt.kubeconfigErr, tt.force, uninstall)
			if called != tt.wantCalled {
				t.Errorf("uninstall called = %v, want %v", called, tt.wantCalled)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if (forced != nil) != tt.wantForced {
				t.Errorf("forced = %v, wantForced %v", forced, tt.wantForced)
			}
		})
	}
}
