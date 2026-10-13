package aws

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/hashicorp/terraform-exec/tfexec"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	testCMKARN   = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testOtherARN = "arn:aws:kms:us-east-1:123456789012:key/0987dcba-09fe-87dc-65ba-ab0987654321"
	testAWSKey   = "arn:aws:kms:us-east-1:123456789012:key/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

type mockEBSDefaultKeyClient struct {
	keyID string
	err   error
}

func (m *mockEBSDefaultKeyClient) GetEbsDefaultKmsKeyId(_ context.Context, _ *ec2.GetEbsDefaultKmsKeyIdInput, _ ...func(*ec2.Options)) (*ec2.GetEbsDefaultKmsKeyIdOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ec2.GetEbsDefaultKmsKeyIdOutput{KmsKeyId: aws.String(m.keyID)}, nil
}

// mockKMSClient resolves key IDs, aliases and ARNs through keys; anything not
// in the map is NotFound.
type mockKMSClient struct {
	keys   map[string]kmstypes.KeyMetadata
	err    error
	called bool
}

func (m *mockKMSClient) DescribeKey(_ context.Context, params *kms.DescribeKeyInput, _ ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	md, ok := m.keys[aws.ToString(params.KeyId)]
	if !ok {
		return nil, &kmstypes.NotFoundException{Message: aws.String("key not found")}
	}
	return &kms.DescribeKeyOutput{KeyMetadata: &md}, nil
}

func customerKey(arn string) kmstypes.KeyMetadata {
	return kmstypes.KeyMetadata{Arn: aws.String(arn), KeyManager: kmstypes.KeyManagerTypeCustomer}
}

func TestDefaultEBSKMSKeyARN(t *testing.T) {
	accessDenied := errors.New("AccessDenied")

	tests := []struct {
		name          string
		ec2           *mockEBSDefaultKeyClient
		kms           *mockKMSClient
		want          string
		wantErr       bool
		wantKMSCalled bool
	}{
		{
			name: "AWS managed alias needs no grant",
			ec2:  &mockEBSDefaultKeyClient{keyID: "alias/aws/ebs"},
			kms:  &mockKMSClient{},
		},
		{
			name: "AWS managed alias ARN needs no grant",
			ec2:  &mockEBSDefaultKeyClient{keyID: "arn:aws:kms:us-east-1:123456789012:alias/aws/ebs"},
			kms:  &mockKMSClient{},
		},
		{
			name:          "customer managed key ARN is returned",
			ec2:           &mockEBSDefaultKeyClient{keyID: testCMKARN},
			kms:           &mockKMSClient{keys: map[string]kmstypes.KeyMetadata{testCMKARN: customerKey(testCMKARN)}},
			want:          testCMKARN,
			wantKMSCalled: true,
		},
		{
			name:          "customer alias is resolved to the key ARN",
			ec2:           &mockEBSDefaultKeyClient{keyID: "arn:aws:kms:us-east-1:123456789012:alias/ebs-default"},
			kms:           &mockKMSClient{keys: map[string]kmstypes.KeyMetadata{"arn:aws:kms:us-east-1:123456789012:alias/ebs-default": customerKey(testCMKARN)}},
			want:          testCMKARN,
			wantKMSCalled: true,
		},
		{
			// What the API returns after ResetEbsDefaultKmsKeyId.
			name: "AWS managed key set by ARN needs no grant",
			ec2:  &mockEBSDefaultKeyClient{keyID: testAWSKey},
			kms: &mockKMSClient{keys: map[string]kmstypes.KeyMetadata{
				testAWSKey: {Arn: aws.String(testAWSKey), KeyManager: kmstypes.KeyManagerTypeAws},
			}},
			wantKMSCalled: true,
		},
		{
			name:          "key ARN is used as is when DescribeKey is denied",
			ec2:           &mockEBSDefaultKeyClient{keyID: testCMKARN},
			kms:           &mockKMSClient{err: accessDenied},
			want:          testCMKARN,
			wantKMSCalled: true,
		},
		{
			name:          "alias that can't be resolved is an error",
			ec2:           &mockEBSDefaultKeyClient{keyID: "alias/ebs-default"},
			kms:           &mockKMSClient{err: accessDenied},
			wantErr:       true,
			wantKMSCalled: true,
		},
		{
			name:    "GetEbsDefaultKmsKeyId failure is an error",
			ec2:     &mockEBSDefaultKeyClient{err: accessDenied},
			kms:     &mockKMSClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defaultEBSKMSKeyARN(context.Background(), tt.ec2, tt.kms)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if tt.kms.called != tt.wantKMSCalled {
				t.Errorf("DescribeKey called = %v, want %v", tt.kms.called, tt.wantKMSCalled)
			}
		})
	}
}

func TestMergeKMSKeyARNs(t *testing.T) {
	tests := []struct {
		name  string
		lists [][]string
		want  []string
	}{
		{name: "nothing", lists: [][]string{nil, {""}}, want: nil},
		{name: "configured only", lists: [][]string{{testCMKARN}, {""}}, want: []string{testCMKARN}},
		{name: "detected only", lists: [][]string{nil, {testCMKARN}}, want: []string{testCMKARN}},
		{name: "configured first, detected appended", lists: [][]string{{testOtherARN}, {testCMKARN}}, want: []string{testOtherARN, testCMKARN}},
		{name: "duplicates dropped", lists: [][]string{{testCMKARN, testCMKARN}, {testCMKARN}}, want: []string{testCMKARN}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeKMSKeyARNs(tt.lists...); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCarryForwardEBSKMSKeyVars covers the deploy whose default-key detection
// failed: the key the last apply recorded must be kept, never revoked.
func TestCarryForwardEBSKMSKeyVars(t *testing.T) {
	detectErr := errors.New("AccessDenied")
	output := func(v string) map[string]tfexec.OutputMeta {
		return map[string]tfexec.OutputMeta{ebsDefaultKMSKeyOutput: {Value: json.RawMessage(v)}}
	}

	tests := []struct {
		name       string
		outputs    map[string]tfexec.OutputMeta
		configured []string
		want       []string
		wantErr    bool
	}{
		{
			name:    "no previous key: nothing to keep",
			outputs: map[string]tfexec.OutputMeta{},
		},
		{
			name:    "previous key is kept",
			outputs: output(`"` + testCMKARN + `"`),
			want: []string{
				`ebs_csi_kms_key_arns=["` + testCMKARN + `"]`,
				`ebs_default_kms_key_arn=` + testCMKARN,
			},
		},
		{
			name:       "previous key is kept after the configured keys",
			outputs:    output(`"` + testCMKARN + `"`),
			configured: []string{testOtherARN},
			want: []string{
				`ebs_csi_kms_key_arns=["` + testOtherARN + `","` + testCMKARN + `"]`,
				`ebs_default_kms_key_arn=` + testCMKARN,
			},
		},
		{
			name:    "malformed output is an error",
			outputs: output(`["not","a","string"]`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan status.Update, 10)
			ctx := status.WithChannel(context.Background(), ch)

			got, err := carryForwardEBSKMSKeyVars(ctx, tt.outputs, tt.configured, detectErr)
			close(ch)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if tt.wantErr {
				return
			}
			var warned bool
			for u := range ch {
				if u.Level == status.LevelWarning {
					warned = true
				}
			}
			if !warned {
				t.Error("expected a warning about the failed detection")
			}
		})
	}
}

func TestValidateKMSKeyARNs(t *testing.T) {
	tests := []struct {
		name    string
		arns    []string
		wantErr bool
	}{
		{name: "empty", arns: nil},
		{name: "key ARNs", arns: []string{testCMKARN, "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/mrk-1234abcd12ab34cd56ef1234567890ab"}},
		{name: "alias ARN", arns: []string{"arn:aws:kms:us-east-1:123456789012:alias/ebs"}, wantErr: true},
		{name: "bare key ID", arns: []string{"1234abcd-12ab-34cd-56ef-1234567890ab"}, wantErr: true},
		{name: "one bad entry", arns: []string{testCMKARN, "alias/ebs"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKMSKeyARNs(tt.arns)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EBSKMSKeyARNs checks Validate rejects a malformed key before it
// reaches the AWS credential check.
func TestValidate_EBSKMSKeyARNs(t *testing.T) {
	cfg := &config.ClusterConfig{Providers: map[string]any{"aws": map[string]any{
		"region":             "us-west-2",
		"kubernetes_version": "1.34",
		"ebs_kms_key_arns":   []any{"alias/ebs-default"},
		"node_groups": map[string]any{
			"general": map[string]any{"instance": "m5.large"},
		},
	}}}

	err := NewProvider().Validate(context.Background(), "test-project", cfg)
	if err == nil || !strings.Contains(err.Error(), "ebs_kms_key_arns") {
		t.Fatalf("expected an ebs_kms_key_arns error, got %v", err)
	}
}

func TestToTFVarsEBSCSIKMSKeyARNs(t *testing.T) {
	tests := []struct {
		name        string
		configured  []string
		detected    string
		wantKeys    []string
		wantDefault string
	}{
		{name: "nothing configured or detected", wantKeys: nil},
		{name: "configured only", configured: []string{testOtherARN}, wantKeys: []string{testOtherARN}},
		{name: "detected only", detected: testCMKARN, wantKeys: []string{testCMKARN}, wantDefault: testCMKARN},
		{name: "configured and detected", configured: []string{testOtherARN}, detected: testCMKARN, wantKeys: []string{testOtherARN, testCMKARN}, wantDefault: testCMKARN},
		{name: "detected key already configured", configured: []string{testCMKARN}, detected: testCMKARN, wantKeys: []string{testCMKARN}, wantDefault: testCMKARN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Region:        "us-east-1",
				NodeGroups:    map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
				EBSKMSKeyARNs: slices.Clone(tt.configured),
			}

			vars := cfg.toTFVars("test", "", nil, tt.detected)
			if !slices.Equal(vars.EBSCSIKMSKeyARNs, tt.wantKeys) {
				t.Errorf("EBSCSIKMSKeyARNs = %v, want %v", vars.EBSCSIKMSKeyARNs, tt.wantKeys)
			}
			if vars.EBSDefaultKMSKeyARN != tt.wantDefault {
				t.Errorf("EBSDefaultKMSKeyARN = %q, want %q", vars.EBSDefaultKMSKeyARN, tt.wantDefault)
			}

			// The merged list must be a fresh slice, never the config's.
			if len(vars.EBSCSIKMSKeyARNs) > 0 {
				vars.EBSCSIKMSKeyARNs[0] = "mutated"
				if !slices.Equal(cfg.EBSKMSKeyARNs, tt.configured) {
					t.Errorf("toTFVars shares its slice with the config: %v", cfg.EBSKMSKeyARNs)
				}
			}
		})
	}
}
