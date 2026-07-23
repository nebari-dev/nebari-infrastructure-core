package aws

import (
	"strings"
	"testing"
)

func TestCrossplaneIAMPolicyGuardrails(t *testing.T) {
	policyBytes, err := tofuTemplates.ReadFile("templates/crossplane-iam.tf")
	if err != nil {
		t.Fatalf("read crossplane IAM template: %v", err)
	}
	policy := string(policyBytes)

	required := []string{
		`name_prefix = "${var.project_name}-apps"`,
		`"arn:aws:s3:::${local.name_prefix}-*"`,
		`"arn:aws:rds:${var.region}:*:db:${local.name_prefix}-*"`,
		`"aws:RequestTag/crossplane-providerconfig" = "aws-${key}"`,
		`"aws:ResourceTag/crossplane-providerconfig" = "aws-${key}"`,
		`tag_actions       = ["rds:AddTagsToResource"]`,
		`untag_actions     = ["rds:RemoveTagsFromResource"]`,
		`length(def.provision_actions) > 0`,
	}
	for _, value := range required {
		if !strings.Contains(policy, value) {
			t.Errorf("crossplane IAM template missing guardrail %q", value)
		}
	}

	forbidden := []string{
		`name_prefix          = "${var.project_name}-crossplane"`,
		`"arn:aws:s3:::${var.project_name}-*"`,
		`"arn:aws:s3:::${local.name_prefix}-*/*"`,
		`provision_actions     = ["rds:CreateDBInstance"`,
	}
	for _, value := range forbidden {
		if strings.Contains(policy, value) {
			t.Errorf("crossplane IAM template contains unsafe policy fragment %q", value)
		}
	}
}
