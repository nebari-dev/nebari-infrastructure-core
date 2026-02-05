package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// mockIAMClient implements IAMClient for testing.
type mockIAMClient struct {
	SimulatePrincipalPolicyFunc func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
}

func (m *mockIAMClient) SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
	if m.SimulatePrincipalPolicyFunc != nil {
		return m.SimulatePrincipalPolicyFunc(ctx, params, optFns...)
	}
	return &iam.SimulatePrincipalPolicyOutput{}, nil
}

func TestMockIAMClient(t *testing.T) {
	// Verify mock implements interface
	var _ IAMClient = &mockIAMClient{}
}
