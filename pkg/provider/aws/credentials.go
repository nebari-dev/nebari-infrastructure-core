package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// IAMClient defines the interface for IAM operations needed for credential validation.
type IAMClient interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
}
