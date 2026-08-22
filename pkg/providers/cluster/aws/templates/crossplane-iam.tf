# Opt-in AWS permissions for application infrastructure provisioned through
# Crossplane. ADR-0012 recommends the dedicated-account model: the AWS account
# is dedicated to one Nebari administrative trust domain, so every Crossplane
# provider controller shares a single, broad, account-local provisioner role
# rather than per-capability least-privilege roles. No static AWS credentials
# are stored in the cluster; the role is assumed via EKS Pod Identity.
#
# The broad grant's blast radius is the whole account -- this is deliberate and
# explicitly not a hard security boundary (ADR-0012 "Security model"). The outer
# denies below are practical guards, in two classes:
#   1. IAM privilege-escalation containment (mandated): the provisioning
#      identity must never mint a runtime identity broader than the workload
#      permissions boundary, and may pass only bucket-scoped workload roles to
#      EKS pods. This keeps provisioning identity separate from runtime identity.
#   2. Accidental-collision guards (best-effort, not a boundary): keep the
#      provider from mutating the foundational EKS control plane it runs on.
#      Foundational OpenTofu resources carry no reliable ownership tag, so name/
#      service separation is the practical -- not absolute -- collision guard.
#
# S3 was verified with provider-aws v2.6.1. RDS remains unverified and may need
# tightly scoped EC2 or KMS permissions once its Composition exists.

locals {
  crossplane_namespace = "crossplane-system"
  # Stable lifecycle namespace independent of the provisioning tool.
  name_prefix = "${var.project_name}-apps"

  # Workload roles the IAM provider creates live under a dedicated IAM path.
  # PassRole and the IAM-write confinement key off this path rather than tags
  # because AWS does not recommend ResourceTag conditions for iam:PassRole.
  crossplane_workload_role_path = "/nebari/${var.project_name}/workloads/"
  crossplane_workload_role_arn  = "arn:aws:iam::*:role${local.crossplane_workload_role_path}*"

  crossplane_enabled            = length(var.crossplane_capabilities) > 0
  object_store_identity_enabled = contains(var.crossplane_capabilities, "s3")
  org_boundary_set              = var.iam_role_permissions_boundary != null && var.iam_role_permissions_boundary != ""

  # Provider controllers that assume the shared provisioner role. Each Crossplane
  # provider package runs as its own ServiceAccount (the aws_eks_pod_identity_
  # association and the DeploymentRuntimeConfig serviceAccountTemplate form the
  # Pod Identity contract with the GitOps manifests), but they all bind to one
  # account-local role. Enabling s3 also installs the IAM and EKS providers that
  # create each ObjectStore's bucket-scoped workload role and Pod Identity binding.
  crossplane_provider_service_accounts = merge(
    local.object_store_identity_enabled ? {
      s3  = "provider-aws-s3"
      iam = "provider-aws-iam"
      eks = "provider-aws-eks"
    } : {},
    contains(var.crossplane_capabilities, "rds") ? {
      rds = "provider-aws-rds"
    } : {},
  )
}

# Maximum runtime permissions for roles created by the IAM provider. This is
# deliberately separate from any organization boundary used by NIC's own
# controller roles: allowing a broader organization boundary to substitute here
# would let a compromised IAM provider grant that broader permission set.
resource "aws_iam_policy" "crossplane_workload_boundary" {
  count       = local.object_store_identity_enabled ? 1 : 0
  name        = "${local.name_prefix}-workload-boundary"
  description = "Maximum permissions for Crossplane-created application workload roles"
  tags        = var.tags
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ObjectStoreBucketAccess"
        Effect = "Allow"
        Action = [
          "s3:GetBucketLocation",
          "s3:ListBucket",
          "s3:ListBucketMultipartUploads",
        ]
        Resource = ["arn:aws:s3:::${local.name_prefix}-*"]
      },
      {
        Sid    = "ObjectStoreObjectAccess"
        Effect = "Allow"
        Action = [
          "s3:AbortMultipartUpload",
          "s3:DeleteObject",
          "s3:GetObject",
          "s3:ListMultipartUploadParts",
          "s3:PutObject",
        ]
        Resource = ["arn:aws:s3:::${local.name_prefix}-*/*"]
      },
      {
        Sid      = "DenyWorkloadPrivilegeEscalation"
        Effect   = "Deny"
        Action   = ["iam:*", "eks:*"]
        Resource = ["*"]
      },
    ]
  })
}

locals {
  crossplane_workload_boundary_arn = local.object_store_identity_enabled ? aws_iam_policy.crossplane_workload_boundary[0].arn : null

  # The broad account-local grant. The dedicated-account model trades
  # least-privilege for a single simple role; the denies that follow are the
  # only carve-outs.
  crossplane_provider_allow = [{
    Sid      = "AccountLocalProvisioning"
    Effect   = "Allow"
    Action   = "*"
    Resource = "*"
  }]

  # IAM privilege-escalation containment. When object storage is enabled the
  # provider creates bucket-scoped workload roles under the reserved path and
  # binds them to pods; every such role must carry the workload boundary, and
  # PassRole is limited to those roles and the Pod Identity service. When object
  # storage is off no workload roles exist, so the provider needs no IAM write
  # or PassRole at all.
  crossplane_iam_denies = local.object_store_identity_enabled ? [
    {
      # All IAM mutation is confined to the workload role path. Reads (GetRole,
      # etc.) stay allowed by the broad grant, so observe-before-create needs no
      # special-case name ARN.
      Sid    = "ConfineIAMWriteToWorkloadPath"
      Effect = "Deny"
      Action = [
        "iam:Attach*",
        "iam:Create*",
        "iam:Delete*",
        "iam:Detach*",
        "iam:Put*",
        "iam:Set*",
        "iam:Tag*",
        "iam:Untag*",
        "iam:Update*",
      ]
      NotResource = [local.crossplane_workload_role_arn]
    },
    {
      # A created workload role (or a boundary change on one) must attach the
      # workload boundary, so the provider cannot mint an unbounded identity.
      Sid      = "RequireWorkloadBoundaryOnRoleCreate"
      Effect   = "Deny"
      Action   = ["iam:CreateRole", "iam:PutRolePermissionsBoundary"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringNotEquals = {
          "iam:PermissionsBoundary" = local.crossplane_workload_boundary_arn
        }
      }
    },
    {
      Sid      = "DenyWorkloadBoundaryRemoval"
      Effect   = "Deny"
      Action   = ["iam:DeleteRolePermissionsBoundary"]
      Resource = ["*"]
    },
    {
      Sid         = "ConfinePassRoleToWorkloadRoles"
      Effect      = "Deny"
      Action      = ["iam:PassRole"]
      NotResource = [local.crossplane_workload_role_arn]
    },
    {
      Sid      = "PassRoleOnlyToPodIdentity"
      Effect   = "Deny"
      Action   = ["iam:PassRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringNotEquals = {
          "iam:PassedToService" = "pods.eks.amazonaws.com"
        }
      }
    },
    ] : [
    {
      # No workload identity is provisioned without object storage, so deny all
      # IAM writes and PassRole outright.
      Sid    = "DenyIAMWrite"
      Effect = "Deny"
      Action = [
        "iam:Attach*",
        "iam:Create*",
        "iam:Delete*",
        "iam:Detach*",
        "iam:PassRole",
        "iam:Put*",
        "iam:Set*",
        "iam:Tag*",
        "iam:Untag*",
        "iam:Update*",
      ]
      Resource = ["*"]
    },
  ]

  # Accidental-collision guard (not a hard boundary): keep the provider from
  # mutating the foundational EKS control plane it runs on. Pod Identity actions
  # stay allowed through the broad grant so the EKS provider can bind workloads.
  crossplane_collision_denies = [
    {
      Sid    = "ProtectFoundationalClusterControlPlane"
      Effect = "Deny"
      Action = [
        "eks:CreateAddon",
        "eks:CreateCluster",
        "eks:CreateFargateProfile",
        "eks:CreateNodegroup",
        "eks:DeleteAddon",
        "eks:DeleteCluster",
        "eks:DeleteFargateProfile",
        "eks:DeleteNodegroup",
        "eks:UpdateAddon",
        "eks:UpdateClusterConfig",
        "eks:UpdateClusterVersion",
        "eks:UpdateNodegroupConfig",
        "eks:UpdateNodegroupVersion",
      ]
      Resource = ["*"]
    },
  ]

  crossplane_provider_statements = concat(
    local.crossplane_provider_allow,
    local.crossplane_iam_denies,
    local.crossplane_collision_denies,
  )
}

# The shared provisioner role is usable only from the Crossplane namespace on
# this cluster. A single ServiceAccount is not pinned because every provider
# controller in crossplane-system shares this role (dedicated-account model).
data "aws_iam_policy_document" "crossplane_provider_trust" {
  count = local.crossplane_enabled ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession"]
    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:RequestTag/eks-cluster-arn"
      values   = [module.eks_cluster.cluster_arn]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:RequestTag/kubernetes-namespace"
      values   = [local.crossplane_namespace]
    }
  }
}

resource "aws_iam_role" "crossplane_provider" {
  count = local.crossplane_enabled ? 1 : 0

  name               = "${local.name_prefix}-crossplane-provider"
  assume_role_policy = data.aws_iam_policy_document.crossplane_provider_trust[0].json
  # The provider role is intentionally broad, so it carries no per-provider
  # permissions boundary. An organization boundary, if configured, still applies.
  permissions_boundary = local.org_boundary_set ? var.iam_role_permissions_boundary : null
  tags                 = var.tags
}

resource "aws_iam_role_policy" "crossplane_provider" {
  count = local.crossplane_enabled ? 1 : 0

  name = "crossplane-provisioner"
  role = aws_iam_role.crossplane_provider[0].id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.crossplane_provider_statements
  })
}

# Bind every provider controller's ServiceAccount to the shared provisioner role.
resource "aws_eks_pod_identity_association" "crossplane_provider" {
  for_each = local.crossplane_provider_service_accounts

  cluster_name    = module.eks_cluster.cluster_name
  namespace       = local.crossplane_namespace
  service_account = each.value
  role_arn        = aws_iam_role.crossplane_provider[0].arn
  tags            = var.tags
}

output "crossplane_provider_role_arn" {
  description = "IAM role assumed by all Crossplane providers via Pod Identity"
  value       = local.crossplane_enabled ? aws_iam_role.crossplane_provider[0].arn : null
}

output "crossplane_workload_boundary_arn" {
  description = "Required permissions boundary for Crossplane-created workload roles"
  value       = local.crossplane_workload_boundary_arn
}

output "crossplane_workload_role_path" {
  description = "Required IAM path for Crossplane-created workload roles"
  value       = local.object_store_identity_enabled ? local.crossplane_workload_role_path : null
}
