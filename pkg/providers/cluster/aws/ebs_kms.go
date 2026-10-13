package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/hashicorp/terraform-exec/tfexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// ebsDefaultKMSKeyOutput is the template variable and output that record the
// default EBS key a deploy detected, so a later deploy that can't detect it
// keeps the grant instead of revoking it.
const ebsDefaultKMSKeyOutput = "ebs_default_kms_key_arn"

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
		span.RecordError(err)
		if kmsKeyARNPattern.MatchString(keyID) {
			return keyID, nil
		}
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

// reportDefaultEBSKMSKey tells the user NIC is granting the EBS CSI driver the
// account's default key. Worded neutrally because the key may have been taken
// as is when DescribeKey was denied, so nobody checked it is customer managed.
func reportDefaultEBSKMSKey(ctx context.Context, arn string) {
	if arn == "" {
		return
	}
	status.Send(ctx, status.NewUpdate(status.LevelInfo,
		"Granting the EBS CSI driver access to the account's default EBS encryption key").
		WithResource("ebs-kms").WithAction("detected").
		WithMetadata("kms_key_arn", arn))
}

// mergeKMSKeyARNs concatenates key lists into a fresh slice, dropping empty
// entries and duplicates while keeping first-seen order, so the policy the
// module renders is stable across deploys. It returns nil when nothing is left.
func mergeKMSKeyARNs(lists ...[]string) []string {
	var merged []string
	for _, arn := range slices.Concat(lists...) {
		if arn != "" && !slices.Contains(merged, arn) {
			merged = append(merged, arn)
		}
	}
	return merged
}

// previousDefaultEBSKMSKeyARN returns the default EBS key the last apply
// granted, read from the ebs_default_kms_key_arn output. It is "" when the
// output is absent: no prior apply, a cluster deployed before NIC recorded
// it, or a deploy that detected no customer managed key.
func previousDefaultEBSKMSKeyARN(outputs map[string]tfexec.OutputMeta) (string, error) {
	out, ok := outputs[ebsDefaultKMSKeyOutput]
	if !ok {
		return "", nil
	}
	var arn string
	if err := json.Unmarshal(out.Value, &arn); err != nil {
		return "", fmt.Errorf("failed to unmarshal %s output: %w", ebsDefaultKMSKeyOutput, err)
	}
	return arn, nil
}

// carryForwardEBSKMSKeyVars handles a deploy whose default-key detection
// failed. Applying without the key would revoke a grant an earlier deploy
// added, and every volume encrypted with it would then fail to attach the
// next time it moves to another node. So it keeps the key the last apply
// granted and returns the -var assignments that override the generated
// tfvars: the merged key list for the module and the default key itself, so
// the output survives for the next deploy. It returns nil when there is
// nothing to keep.
func carryForwardEBSKMSKeyVars(ctx context.Context, outputs map[string]tfexec.OutputMeta, configured []string, detectErr error) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.carryForwardEBSKMSKeyVars")
	defer span.End()

	previous, err := previousDefaultEBSKMSKeyARN(outputs)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.String("ebs.previous_default_kms_key_arn", previous))

	if previous == "" {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("Could not detect the account's default EBS encryption key (%v). If it is a customer managed key, add it to ebs_kms_key_arns or the EBS CSI driver can't create or attach volumes encrypted with it", detectErr)).
			WithResource("ebs-kms").WithAction("detecting"))
		return nil, nil
	}

	keys, err := json.Marshal(mergeKMSKeyARNs(configured, []string{previous}))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to marshal EBS CSI KMS key ARNs: %w", err)
	}
	status.Send(ctx, status.NewUpdate(status.LevelWarning,
		fmt.Sprintf("Could not detect the account's default EBS encryption key (%v); keeping the key the previous deploy granted the EBS CSI driver", detectErr)).
		WithResource("ebs-kms").WithAction("detecting").
		WithMetadata("kms_key_arn", previous))
	return []string{
		"ebs_csi_kms_key_arns=" + string(keys),
		ebsDefaultKMSKeyOutput + "=" + previous,
	}, nil
}
