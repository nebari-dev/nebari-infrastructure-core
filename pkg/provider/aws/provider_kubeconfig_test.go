package aws

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

type fakeTF struct {
	initErr    error
	outputErr  error
	outputs    map[string]tfexec.OutputMeta
	cleanupErr error
}

func (f *fakeTF) Init(ctx context.Context, opts ...tfexec.InitOption) error { return f.initErr }
func (f *fakeTF) Output(ctx context.Context, _ ...tfexec.OutputOption) (map[string]tfexec.OutputMeta, error) {
	if f.outputErr != nil {
		return nil, f.outputErr
	}
	return f.outputs, nil
}
func (f *fakeTF) Cleanup() error { return f.cleanupErr }

func TestGetKubeconfig_NewCluster_BuildsFromTFOutputs(t *testing.T) {
	ctx := context.Background()
	p := &Provider{}

	origExtract := extractAWSConfigFn
	origTofu := tofuSetupFn
	t.Cleanup(func() {
		extractAWSConfigFn = origExtract
		tofuSetupFn = origTofu
	})

	extractAWSConfigFn = func(ctx context.Context, cfg *config.NebariConfig) (*Config, error) {
		// StateBucket set -> avoids STS path in GetKubeconfig
		return &Config{Region: "us-east-1", StateBucket: "test-bucket"}, nil
	}

	endpointJSON, _ := json.Marshal("https://example.eks.amazonaws.com")
	caJSON, _ := json.Marshal("LS0tLS1CRUdJTi0tLS0t") // dummy base64-ish string

	tf := &fakeTF{
		outputs: map[string]tfexec.OutputMeta{
			"cluster_endpoint":                   {Value: endpointJSON},
			"cluster_certificate_authority_data": {Value: caJSON},
		},
	}

	tofuSetupFn = func(ctx context.Context, templates fs.FS, tfvars any) (tfClient, error) {
		return tf, nil
	}

	cfg := &config.NebariConfig{ProjectName: "myproj"}

	got, err := p.GetKubeconfig(ctx, cfg)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	s := string(got)
	mustContain(t, s, "myproj")
	mustContain(t, s, "https://example.eks.amazonaws.com")
	mustContain(t, s, "us-east-1")
}

func TestGetKubeconfig_NewCluster_MissingEndpoint_ReturnsHelpfulError(t *testing.T) {
	ctx := context.Background()
	p := &Provider{}

	origExtract := extractAWSConfigFn
	origTofu := tofuSetupFn
	t.Cleanup(func() {
		extractAWSConfigFn = origExtract
		tofuSetupFn = origTofu
	})

	extractAWSConfigFn = func(ctx context.Context, cfg *config.NebariConfig) (*Config, error) {
		return &Config{Region: "us-east-1", StateBucket: "test-bucket"}, nil
	}

	tf := &fakeTF{
		outputs: map[string]tfexec.OutputMeta{
			// missing cluster_endpoint
			"cluster_certificate_authority_data": {Value: []byte(`"abc"`)},
		},
	}

	tofuSetupFn = func(ctx context.Context, templates fs.FS, tfvars any) (tfClient, error) {
		return tf, nil
	}

	cfg := &config.NebariConfig{ProjectName: "myproj"}
	_, err := p.GetKubeconfig(ctx, cfg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	mustContain(t, err.Error(), "cluster not found")
	mustContain(t, err.Error(), "run 'deploy' first")
}

func TestGetKubeconfig_NewCluster_MissingCA_ReturnsHelpfulError(t *testing.T) {
	ctx := context.Background()
	p := &Provider{}

	origExtract := extractAWSConfigFn
	origTofu := tofuSetupFn
	t.Cleanup(func() {
		extractAWSConfigFn = origExtract
		tofuSetupFn = origTofu
	})

	extractAWSConfigFn = func(ctx context.Context, cfg *config.NebariConfig) (*Config, error) {
		return &Config{Region: "us-east-1", StateBucket: "test-bucket"}, nil
	}

	tf := &fakeTF{
		outputs: map[string]tfexec.OutputMeta{
			"cluster_endpoint": {Value: []byte(`"https://example.eks.amazonaws.com"`)},
			// missing cluster_certificate_authority_data
		},
	}

	tofuSetupFn = func(ctx context.Context, templates fs.FS, tfvars any) (tfClient, error) {
		return tf, nil
	}

	cfg := &config.NebariConfig{ProjectName: "myproj"}
	_, err := p.GetKubeconfig(ctx, cfg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	mustContain(t, err.Error(), "cluster not found")
	mustContain(t, err.Error(), "run 'deploy' first")
}

func TestGetKubeconfig_NewCluster_TofuOutputError_ReturnsWrappedError(t *testing.T) {
	ctx := context.Background()
	p := &Provider{}

	origExtract := extractAWSConfigFn
	origTofu := tofuSetupFn
	t.Cleanup(func() {
		extractAWSConfigFn = origExtract
		tofuSetupFn = origTofu
	})

	extractAWSConfigFn = func(ctx context.Context, cfg *config.NebariConfig) (*Config, error) {
		return &Config{Region: "us-east-1", StateBucket: "test-bucket"}, nil
	}

	tf := &fakeTF{outputErr: errors.New("boom")}
	tofuSetupFn = func(ctx context.Context, templates fs.FS, tfvars any) (tfClient, error) {
		return tf, nil
	}

	cfg := &config.NebariConfig{ProjectName: "myproj"}
	_, err := p.GetKubeconfig(ctx, cfg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	mustContain(t, err.Error(), "failed to get terraform outputs")
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("expected to contain %q, got:\n%s", sub, s)
	}
}
