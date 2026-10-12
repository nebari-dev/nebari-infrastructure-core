package aws

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

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

func TestEBSCSIKMSKeyARNs(t *testing.T) {
	cmk := &mockKMSClient{keys: map[string]kmstypes.KeyMetadata{testCMKARN: customerKey(testCMKARN)}}

	tests := []struct {
		name        string
		ec2         *mockEBSDefaultKeyClient
		configured  []string
		want        []string
		wantWarning bool
	}{
		{
			name: "AWS managed default and nothing configured",
			ec2:  &mockEBSDefaultKeyClient{keyID: "alias/aws/ebs"},
			want: nil,
		},
		{
			name: "customer managed default is added",
			ec2:  &mockEBSDefaultKeyClient{keyID: testCMKARN},
			want: []string{testCMKARN},
		},
		{
			name:       "configured keys come first and the default is appended",
			ec2:        &mockEBSDefaultKeyClient{keyID: testCMKARN},
			configured: []string{testOtherARN},
			want:       []string{testOtherARN, testCMKARN},
		},
		{
			name:       "default already configured is not duplicated",
			ec2:        &mockEBSDefaultKeyClient{keyID: testCMKARN},
			configured: []string{testCMKARN},
			want:       []string{testCMKARN},
		},
		{
			name:        "detection failure keeps the configured keys and warns",
			ec2:         &mockEBSDefaultKeyClient{err: errors.New("AccessDenied")},
			configured:  []string{testOtherARN},
			want:        []string{testOtherARN},
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan status.Update, 10)
			ctx := status.WithChannel(context.Background(), ch)
			configured := slices.Clone(tt.configured)

			got := ebsCSIKMSKeyARNs(ctx, tt.ec2, cmk, tt.configured)
			close(ch)

			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if !slices.Equal(tt.configured, configured) {
				t.Errorf("configured keys were mutated: %v", tt.configured)
			}
			var warned bool
			for u := range ch {
				if u.Level == status.LevelWarning {
					warned = true
				}
			}
			if warned != tt.wantWarning {
				t.Errorf("warning sent = %v, want %v", warned, tt.wantWarning)
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
	cfg := Config{
		Region:        "us-east-1",
		NodeGroups:    map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
		EBSKMSKeyARNs: []string{testCMKARN},
	}

	vars := cfg.toTFVars("test", "", nil)
	if !slices.Equal(vars.EBSCSIKMSKeyARNs, []string{testCMKARN}) {
		t.Fatalf("EBSCSIKMSKeyARNs = %v, want [%s]", vars.EBSCSIKMSKeyARNs, testCMKARN)
	}

	// Deploy edits the tfvars list; that must not write through to the config.
	vars.EBSCSIKMSKeyARNs[0] = testOtherARN
	if cfg.EBSKMSKeyARNs[0] != testCMKARN {
		t.Errorf("toTFVars shares its slice with the config: %v", cfg.EBSKMSKeyARNs)
	}

	cfg.EBSKMSKeyARNs = nil
	if vars := cfg.toTFVars("test", "", nil); vars.EBSCSIKMSKeyARNs != nil {
		t.Errorf("expected nil EBSCSIKMSKeyARNs when unset, got %v", vars.EBSCSIKMSKeyARNs)
	}
}
