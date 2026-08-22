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
		// Stable naming and the reserved workload-role path.
		`name_prefix = "${var.project_name}-apps"`,
		`crossplane_workload_role_path = "/nebari/${var.project_name}/workloads/"`,
		`crossplane_workload_role_arn  = "arn:aws:iam::*:role${local.crossplane_workload_role_path}*"`,
		// The workload permissions boundary bounds runtime identity.
		`resource "aws_iam_policy" "crossplane_workload_boundary"`,
		`"arn:aws:s3:::${local.name_prefix}-*/*"`,
		// One broad, account-local provider role shared by all controllers.
		`Sid      = "AccountLocalProvisioning"`,
		`Action   = "*"`,
		`name               = "${local.name_prefix}-crossplane-provider"`,
		// IAM privilege-escalation containment: confine writes to the workload
		// path, force the boundary on create, and scope PassRole.
		`Sid    = "ConfineIAMWriteToWorkloadPath"`,
		`NotResource = [local.crossplane_workload_role_arn]`,
		`Sid      = "RequireWorkloadBoundaryOnRoleCreate"`,
		`"iam:PermissionsBoundary" = local.crossplane_workload_boundary_arn`,
		`Sid      = "PassRoleOnlyToPodIdentity"`,
		`"iam:PassedToService" = "pods.eks.amazonaws.com"`,
		// Practical collision guard for the foundational control plane.
		`Sid    = "ProtectFoundationalClusterControlPlane"`,
		`"eks:DeleteCluster"`,
		// Pod Identity trust is scoped to the cluster and the Crossplane namespace.
		`"aws:RequestTag/eks-cluster-arn"`,
		`"aws:RequestTag/kubernetes-namespace"`,
		`resource "aws_eks_pod_identity_association" "crossplane_provider"`,
	}
	for _, value := range required {
		if !strings.Contains(policy, value) {
			t.Errorf("crossplane IAM template missing guardrail %q", value)
		}
	}

	forbidden := []string{
		// Strict-separation remnants: per-capability roles, scoped policies, and
		// per-provider boundaries were replaced by the single broad role.
		`resource "aws_iam_policy" "crossplane_provider_boundary"`,
		`crossplane_resource_capability_defs`,
		`"aws:RequestTag/crossplane-providerconfig"`,
		`"aws:ResourceTag/crossplane-providerconfig"`,
		// The shared role trusts the whole Crossplane namespace, not one SA.
		`"aws:RequestTag/kubernetes-service-account"`,
		// Generic unsafe fragments.
		`Resource = ["arn:aws:iam::*:role/*"]`,
	}
	for _, value := range forbidden {
		if strings.Contains(policy, value) {
			t.Errorf("crossplane IAM template contains unsafe policy fragment %q", value)
		}
	}

	// PassRole must be confined (to workload roles) and further restricted to the
	// Pod Identity service -- never granted unconditionally on all resources.
	for _, sid := range []string{
		`Sid         = "ConfinePassRoleToWorkloadRoles"`,
		`Sid      = "PassRoleOnlyToPodIdentity"`,
	} {
		if !strings.Contains(policy, sid) {
			t.Errorf("crossplane IAM template missing PassRole guard %q", sid)
		}
	}
}
