package aws

import (
	"context"
	"strings"
	"testing"
)

func TestParseK8sVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		wantMajor   int
		wantMinor   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid version 1.34",
			version:   "1.34",
			wantMajor: 1,
			wantMinor: 34,
			wantErr:   false,
		},
		{
			name:      "valid version 1.29",
			version:   "1.29",
			wantMajor: 1,
			wantMinor: 29,
			wantErr:   false,
		},
		{
			name:      "valid version 1.30",
			version:   "1.30",
			wantMajor: 1,
			wantMinor: 30,
			wantErr:   false,
		},
		{
			name:        "invalid format - no dot",
			version:     "128",
			wantErr:     true,
			errContains: "must be in format",
		},
		{
			name:        "invalid format - only major",
			version:     "1",
			wantErr:     true,
			errContains: "must be in format",
		},
		{
			name:        "invalid major version",
			version:     "x.28",
			wantErr:     true,
			errContains: "invalid major version",
		},
		{
			name:        "invalid minor version",
			version:     "1.x",
			wantErr:     true,
			errContains: "invalid minor version",
		},
		{
			name:        "empty version",
			version:     "",
			wantErr:     true,
			errContains: "must be in format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, err := parseK8sVersion(tt.version)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseK8sVersion() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("parseK8sVersion() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parseK8sVersion() unexpected error = %v", err)
				return
			}

			if major != tt.wantMajor {
				t.Errorf("parseK8sVersion() major = %v, want %v", major, tt.wantMajor)
			}

			if minor != tt.wantMinor {
				t.Errorf("parseK8sVersion() minor = %v, want %v", minor, tt.wantMinor)
			}
		})
	}
}

func TestValidateK8sVersionUpgrade(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		current     string
		desired     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "no upgrade - same version",
			current: "1.34",
			desired: "1.34",
			wantErr: false,
		},
		{
			name:    "valid upgrade - one minor version",
			current: "1.33",
			desired: "1.34",
			wantErr: false,
		},
		{
			name:    "valid upgrade - 1.29 to 1.30",
			current: "1.29",
			desired: "1.30",
			wantErr: false,
		},
		{
			name:        "invalid upgrade - skip minor version",
			current:     "1.32",
			desired:     "1.34",
			wantErr:     true,
			errContains: "cannot skip Kubernetes minor versions",
		},
		{
			name:        "invalid upgrade - skip multiple minor versions",
			current:     "1.27",
			desired:     "1.30",
			wantErr:     true,
			errContains: "cannot skip Kubernetes minor versions",
		},
		{
			name:        "invalid - downgrade",
			current:     "1.34",
			desired:     "1.33",
			wantErr:     true,
			errContains: "cannot downgrade",
		},
		{
			name:        "invalid - major version change",
			current:     "1.34",
			desired:     "2.0",
			wantErr:     true,
			errContains: "cannot change Kubernetes major version",
		},
		{
			name:        "invalid current version format",
			current:     "invalid",
			desired:     "1.34",
			wantErr:     true,
			errContains: "invalid current Kubernetes version",
		},
		{
			name:        "invalid desired version format",
			current:     "1.34",
			desired:     "invalid",
			wantErr:     true,
			errContains: "invalid desired Kubernetes version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateK8sVersionUpgrade(ctx, tt.current, tt.desired)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateK8sVersionUpgrade() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateK8sVersionUpgrade() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("validateK8sVersionUpgrade() unexpected error = %v", err)
			}
		})
	}
}

func TestValidateK8sVersionUpgrade_EdgeCases(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		current     string
		desired     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "upgrade from old version",
			current: "1.24",
			desired: "1.25",
			wantErr: false,
		},
		{
			name:    "upgrade to newer version",
			current: "1.30",
			desired: "1.31",
			wantErr: false,
		},
		{
			name:        "large version gap",
			current:     "1.20",
			desired:     "1.30",
			wantErr:     true,
			errContains: "cannot skip Kubernetes minor versions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateK8sVersionUpgrade(ctx, tt.current, tt.desired)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateK8sVersionUpgrade() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateK8sVersionUpgrade() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("validateK8sVersionUpgrade() unexpected error = %v", err)
			}
		})
	}
}
