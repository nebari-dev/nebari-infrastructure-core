package registry

import (
	"context"
	"strings"
	"testing"
)

func TestRegister(t *testing.T) {
	tests := []struct {
		name        string
		entries     []string
		wantErr     bool
		errContains string
	}{
		{
			name:    "single entry",
			entries: []string{"aws"},
		},
		{
			name:    "multiple entries",
			entries: []string{"aws", "gcp", "azure"},
		},
		{
			name:        "duplicate fails",
			entries:     []string{"aws", "aws"},
			wantErr:     true,
			errContains: "already registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := New[string]()

			var err error
			for _, name := range tt.entries {
				err = reg.Register(ctx, name, name+"-value")
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

func TestGet(t *testing.T) {
	tests := []struct {
		name        string
		register    []string
		lookup      string
		wantErr     bool
		errContains string
	}{
		{
			name:     "existing entry",
			register: []string{"aws"},
			lookup:   "aws",
		},
		{
			name:        "non-existent entry",
			register:    []string{"aws"},
			lookup:      "gcp",
			wantErr:     true,
			errContains: "not registered",
		},
		{
			name:        "empty registry",
			register:    []string{},
			lookup:      "aws",
			wantErr:     true,
			errContains: "not registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := New[string]()

			for _, name := range tt.register {
				if err := reg.Register(ctx, name, name+"-value"); err != nil {
					t.Fatalf("setup: Register(%q) failed: %v", name, err)
				}
			}

			got, err := reg.Get(ctx, tt.lookup)

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
			if got != tt.lookup+"-value" {
				t.Errorf("got %q, want %q", got, tt.lookup+"-value")
			}
		})
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name     string
		register []string
		want     int
	}{
		{
			name:     "empty registry",
			register: []string{},
			want:     0,
		},
		{
			name:     "single entry",
			register: []string{"aws"},
			want:     1,
		},
		{
			name:     "multiple entries",
			register: []string{"aws", "gcp", "azure"},
			want:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := New[string]()

			for _, name := range tt.register {
				if err := reg.Register(ctx, name, name+"-value"); err != nil {
					t.Fatalf("setup: Register(%q) failed: %v", name, err)
				}
			}

			got := reg.List(ctx)
			if len(got) != tt.want {
				t.Fatalf("List() returned %d entries, want %d", len(got), tt.want)
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

func TestErrorIncludesTypeName(t *testing.T) {
	ctx := context.Background()
	reg := New[int]()

	if err := reg.Register(ctx, "cf", 1); err != nil {
		t.Fatal(err)
	}

	// Duplicate register error should mention the type derived from %T
	err := reg.Register(ctx, "cf", 2)
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Errorf("expected error containing %q, got %v", "int", err)
	}

	// Get not-found error should mention the type
	_, err = reg.Get(ctx, "nope")
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Errorf("expected error containing %q, got %v", "int", err)
	}
}
