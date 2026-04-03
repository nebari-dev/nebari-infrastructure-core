package registry

import (
	"context"
	"strings"
	"testing"
)

// named is a minimal interface for testing ProviderList with a concrete type.
type named interface {
	Name() string
}

// stubProvider implements named for test purposes.
type stubProvider struct {
	name string
}

func (s *stubProvider) Name() string { return s.name }

func TestProviderList_Register(t *testing.T) {
	tests := []struct {
		name        string
		providers   []string
		wantErr     bool
		errContains string
	}{
		{
			name:      "single provider",
			providers: []string{"aws"},
		},
		{
			name:      "multiple providers",
			providers: []string{"aws", "gcp", "azure"},
		},
		{
			name:        "duplicate registration fails",
			providers:   []string{"aws", "aws"},
			wantErr:     true,
			errContains: "already registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pl := newProviderList[named]("TestProviders")

			var err error
			for _, name := range tt.providers {
				err = pl.Register(ctx, name, &stubProvider{name: name})
			}

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProviderList_Get(t *testing.T) {
	tests := []struct {
		name        string
		register    []string
		lookup      string
		wantErr     bool
		errContains string
	}{
		{
			name:     "existing provider",
			register: []string{"aws"},
			lookup:   "aws",
		},
		{
			name:        "non-existent provider",
			register:    []string{"aws"},
			lookup:      "gcp",
			wantErr:     true,
			errContains: "not registered",
		},
		{
			name:        "empty list",
			register:    []string{},
			lookup:      "aws",
			wantErr:     true,
			errContains: "not registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pl := newProviderList[named]("TestProviders")

			for _, name := range tt.register {
				if err := pl.Register(ctx, name, &stubProvider{name: name}); err != nil {
					t.Fatalf("setup: Register(%q) failed: %v", name, err)
				}
			}

			got, err := pl.Get(ctx, tt.lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name() != tt.lookup {
				t.Errorf("got provider %q, want %q", got.Name(), tt.lookup)
			}
		})
	}
}

func TestProviderList_List(t *testing.T) {
	tests := []struct {
		name     string
		register []string
		want     int
	}{
		{
			name:     "empty list",
			register: []string{},
			want:     0,
		},
		{
			name:     "single provider",
			register: []string{"aws"},
			want:     1,
		},
		{
			name:     "multiple providers",
			register: []string{"aws", "gcp", "azure"},
			want:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pl := newProviderList[named]("TestProviders")

			for _, name := range tt.register {
				if err := pl.Register(ctx, name, &stubProvider{name: name}); err != nil {
					t.Fatalf("setup: Register(%q) failed: %v", name, err)
				}
			}

			got := pl.List(ctx)
			if len(got) != tt.want {
				t.Fatalf("List() returned %d names, want %d", len(got), tt.want)
			}

			found := make(map[string]bool)
			for _, name := range got {
				found[name] = true
			}
			for _, name := range tt.register {
				if !found[name] {
					t.Errorf("List() missing %q", name)
				}
			}
		})
	}
}
