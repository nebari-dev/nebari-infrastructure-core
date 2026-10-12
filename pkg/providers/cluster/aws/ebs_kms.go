package aws

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// awsManagedEBSKeyAlias is what GetEbsDefaultKmsKeyId returns when the account
// has never chosen a default EBS key. Its key policy already lets every
// principal in the account use it through EBS, so the CSI driver needs nothing
// extra. After ResetEbsDefaultKmsKeyId the API returns that same key's ARN
// instead, which DescribeKey reports as AWS managed.
const awsManagedEBSKeyAlias = "alias/aws/ebs"

// kmsKeyARNPattern matches a KMS key ARN. Alias ARNs are deliberately excluded:
// IAM evaluates KMS permissions against the key ARN, so an alias ARN in the CSI
// driver's policy would grant nothing.
var kmsKeyARNPattern = regexp.MustCompile(`^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[A-Za-z0-9-]+$`)

// EBSDefaultKeyClient is the EC2 call used to read the account's default EBS
// encryption key.
type EBSDefaultKeyClient interface {
	GetEbsDefaultKmsKeyId(ctx context.Context, params *ec2.GetEbsDefaultKmsKeyIdInput, optFns ...func(*ec2.Options)) (*ec2.GetEbsDefaultKmsKeyIdOutput, error)
}

// KMSClient is the KMS call used to resolve a key ID or alias to its ARN and
// tell AWS managed keys apart from customer managed ones.
type KMSClient interface {
	DescribeKey(ctx context.Context, params *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
}

func newEBSDefaultKeyClients(ctx context.Context, region string) (EBSDefaultKeyClient, KMSClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return ec2.NewFromConfig(cfg), kms.NewFromConfig(cfg), nil
}

// validateKMSKeyARNs checks that every configured EBS key is a KMS key ARN.
func validateKMSKeyARNs(arns []string) error {
	for _, arn := range arns {
		if !kmsKeyARNPattern.MatchString(arn) {
			return fmt.Errorf("ebs_kms_key_arns: %q is not a KMS key ARN (arn:<partition>:kms:<region>:<account>:key/<key-id>); alias ARNs are not supported", arn)
		}
	}
	return nil
}

// defaultEBSKMSKeyARN returns the ARN of the account's default EBS encryption
// key in the client's region when that key is customer managed, or "" when it
// is the AWS managed aws/ebs key. The default key applies to every volume the
// EBS CSI driver creates in an account with encryption by default turned on,
// and to any StorageClass that sets encrypted=true without a kmsKeyId.
func defaultEBSKMSKeyARN(ctx context.Context, ec2Client EBSDefaultKeyClient, kmsClient KMSClient) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.defaultEBSKMSKeyARN")
	defer span.End()

	out, err := ec2Client.GetEbsDefaultKmsKeyId(ctx, &ec2.GetEbsDefaultKmsKeyIdInput{})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to read the default EBS KMS key: %w", err)
	}
	keyID := aws.ToString(out.KmsKeyId)
	span.SetAttributes(attribute.String("ebs.default_kms_key_id", keyID))
	if keyID == "" || keyID == awsManagedEBSKeyAlias || strings.HasSuffix(keyID, ":"+awsManagedEBSKeyAlias) {
		return "", nil
	}

	desc, err := kmsClient.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(keyID)})
	if err != nil {
		// Without DescribeKey we can't tell AWS managed from customer managed,
		// but a key ARN is usable as is, and granting the driver an AWS managed
		// key is harmless. An alias or bare key ID can't go in the policy.
		if kmsKeyARNPattern.MatchString(keyID) {
			return keyID, nil
		}
		span.RecordError(err)
		return "", fmt.Errorf("failed to resolve default EBS KMS key %s: %w", keyID, err)
	}
	if desc.KeyMetadata == nil || desc.KeyMetadata.Arn == nil {
		err := fmt.Errorf("DescribeKey returned no ARN for default EBS KMS key %s", keyID)
		span.RecordError(err)
		return "", err
	}
	if desc.KeyMetadata.KeyManager == kmstypes.KeyManagerTypeAws {
		return "", nil
	}

	arn := aws.ToString(desc.KeyMetadata.Arn)
	span.SetAttributes(attribute.String("ebs.default_kms_key_arn", arn))
	return arn, nil
}

// ebsCSIKMSKeyARNs returns the KMS keys the EBS CSI driver's role should be
// allowed to use: the configured ebs_kms_key_arns plus the account's default
// EBS key when it is customer managed. Detection runs on every deploy so a
// changed account default is picked up. A detection failure is reported as a
// warning and the configured keys are returned unchanged, so a deploy
// principal without ec2:GetEbsDefaultKmsKeyId or kms:DescribeKey can still
// deploy (and set ebs_kms_key_arns by hand).
func ebsCSIKMSKeyARNs(ctx context.Context, ec2Client EBSDefaultKeyClient, kmsClient KMSClient, configured []string) []string {
	keys := slices.Clone(configured)

	detected, err := defaultEBSKMSKeyARN(ctx, ec2Client, kmsClient)
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("Could not detect the account's default EBS encryption key (%v). If it is a customer managed key, add it to ebs_kms_key_arns or EBS volumes will fail to provision", err)).
			WithResource("ebs-kms").WithAction("detecting"))
		return keys
	}
	if detected == "" || slices.Contains(keys, detected) {
		return keys
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo,
		"Account's default EBS encryption key is customer managed; granting the EBS CSI driver access to it").
		WithResource("ebs-kms").WithAction("detected").
		WithMetadata("kms_key_arn", detected))
	return append(keys, detected)
}
