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
		`"arn:aws:s3:::${local.name_prefix}-*/*"`,
		`"arn:aws:rds:${var.region}:*:db:${local.name_prefix}-*"`,
		`"aws:RequestTag/crossplane-providerconfig" = "aws-${key}"`,
		`"aws:ResourceTag/crossplane-providerconfig" = "aws-${key}"`,
		`tag_actions       = ["rds:AddTagsToResource"]`,
		`untag_actions     = ["rds:RemoveTagsFromResource"]`,
		`length(def.provision_actions) > 0`,
		`crossplane_workload_role_path = "/nebari/${var.project_name}/workloads/"`,
		`resource "aws_iam_policy" "crossplane_workload_boundary"`,
		`resource "aws_iam_policy" "crossplane_provider_boundary"`,
		`"iam:PermissionsBoundary"`,
		`"iam:ResourceTag/crossplane-providerconfig" = "aws-iam"`,
		`Action   = ["iam:PassRole"]`,
		`"iam:PassedToService" = "pods.eks.amazonaws.com"`,
		`Action   = ["eks:CreatePodIdentityAssociation"]`,
		`"aws:RequestTag/eks-cluster-arn"`,
		`"aws:RequestTag/kubernetes-namespace"`,
		`"aws:RequestTag/kubernetes-service-account"`,
	}
	for _, value := range required {
		if !strings.Contains(policy, value) {
			t.Errorf("crossplane IAM template missing guardrail %q", value)
		}
	}

	forbidden := []string{
		`name_prefix          = "${var.project_name}-crossplane"`,
		`"arn:aws:s3:::${var.project_name}-*"`,
		`provision_actions     = ["rds:CreateDBInstance"`,
		`Resource = ["arn:aws:iam::*:role/*"]`,
		`Resource = ["arn:aws:eks:${var.region}:*:cluster/*"]`,
	}
	for _, value := range forbidden {
		if strings.Contains(policy, value) {
			t.Errorf("crossplane IAM template contains unsafe policy fragment %q", value)
		}
	}

	if got := strings.Count(policy, `"iam:PassRole"`); got != 1 {
		t.Errorf("crossplane IAM template contains %d iam:PassRole grants, want exactly 1", got)
	}
}
