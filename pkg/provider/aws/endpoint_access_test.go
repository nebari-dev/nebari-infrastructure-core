package aws

import (
	"context"
	"testing"
)

func TestGetEndpointAccessConfig(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name              string
		endpointAccess    string
		wantPublicAccess  bool
		wantPrivateAccess bool
	}{
		{
			name:              "public access (explicit)",
			endpointAccess:    "public",
			wantPublicAccess:  true,
			wantPrivateAccess: false,
		},
		{
			name:              "private access",
			endpointAccess:    "private",
			wantPublicAccess:  false,
			wantPrivateAccess: true,
		},
		{
			name:              "public and private access",
			endpointAccess:    "public-and-private",
			wantPublicAccess:  true,
			wantPrivateAccess: true,
		},
		{
			name:              "default (empty string)",
			endpointAccess:    "",
			wantPublicAccess:  true,
			wantPrivateAccess: true,
		},
		{
			name:              "invalid value defaults to public-and-private",
			endpointAccess:    "invalid",
			wantPublicAccess:  true,
			wantPrivateAccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := getEndpointAccessConfig(ctx, tt.endpointAccess)

			if config.PublicAccess != tt.wantPublicAccess {
				t.Errorf("getEndpointAccessConfig() PublicAccess = %v, want %v", config.PublicAccess, tt.wantPublicAccess)
			}

			if config.PrivateAccess != tt.wantPrivateAccess {
				t.Errorf("getEndpointAccessConfig() PrivateAccess = %v, want %v", config.PrivateAccess, tt.wantPrivateAccess)
			}
		})
	}
}

func TestEndpointAccessConfig_DefaultValues(t *testing.T) {
	ctx := context.Background()

	// Test that defaults match the constants
	config := getEndpointAccessConfig(ctx, "")

	if config.PublicAccess != DefaultEndpointPublic {
		t.Errorf("Default PublicAccess = %v, want %v", config.PublicAccess, DefaultEndpointPublic)
	}

	if config.PrivateAccess != DefaultEndpointPrivate {
		t.Errorf("Default PrivateAccess = %v, want %v", config.PrivateAccess, DefaultEndpointPrivate)
	}
}
