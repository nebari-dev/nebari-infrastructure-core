# Opt-in AWS permissions for application infrastructure provisioned through
# Crossplane. Each capability gets a separate EKS Pod Identity role and no
# static AWS credentials are stored in the cluster.
#
# Cloud resources must use the tool-neutral `${project_name}-apps-*` lifecycle
# namespace; foundational OpenTofu resources must not. Mutations and deletion
# additionally require the provider's `crossplane-providerconfig` ownership tag.
#
# Tags are not immutable creation provenance, so reserve the apps prefix and
# prevent workload users from adopting resources through
# `crossplane.io/external-name`. Packs intentionally share each capability role;
# pack isolation belongs in Kubernetes RBAC and admission policy.
#
# S3 was verified with provider-aws v2.6.1. RDS remains unverified and may need
# tightly scoped EC2, KMS, or PassRole permissions.

locals {
  crossplane_namespace = "crossplane-system"
  # Stable lifecycle namespace independent of the provisioning tool.
  name_prefix = "${var.project_name}-apps"

  # service_account and the aws-<key> ProviderConfig name form the Pod Identity
  # and ownership-tag contracts with the GitOps manifests.
  crossplane_capability_defs = {
    s3 = {
      service_account = "provider-aws-s3"
      resource_arns = [
        "arn:aws:s3:::${local.name_prefix}-*",
      ]
      # Bucket discovery cannot be resource-scoped.
      observe_actions = ["s3:ListAllMyBuckets", "s3:GetBucketLocation"]
      # CreateBucket supplies the ownership tag and requires TagResource.
      create_actions = ["s3:CreateBucket", "s3:TagResource"]
      # Bootstrap reads and PutBucketAbac run before resource-tag conditions work.
      # Only bucket ARNs are allowed, so GetObject remains denied. BucketAbac
      # compositions must omit Delete to keep ABAC active until Bucket deletion.
      provision_actions = ["s3:PutBucketAbac", "s3:Get*", "s3:List*"]
      manage_actions    = ["s3:*"]
      tag_actions       = ["s3:TagResource"]
      untag_actions     = ["s3:UntagResource"]
    }
    rds = {
      service_account = "provider-aws-rds"
      resource_arns = [
        "arn:aws:rds:${var.region}:*:db:${local.name_prefix}-*",
        "arn:aws:rds:${var.region}:*:subgrp:${local.name_prefix}-*",
        "arn:aws:rds:${var.region}:*:pg:${local.name_prefix}-*",
        "arn:aws:rds:${var.region}:*:og:${local.name_prefix}-*",
        "arn:aws:rds:${var.region}:*:secgrp:${local.name_prefix}-*",
      ]
      # RDS discovery largely cannot be resource-scoped.
      observe_actions = ["rds:Describe*", "rds:ListTagsForResource"]
      # AddTagsToResource is a dependent create action; all creates require the
      # ownership request tag.
      create_actions = [
        "rds:CreateDBInstance",
        "rds:CreateDBSubnetGroup",
        "rds:CreateDBParameterGroup",
        "rds:AddTagsToResource",
      ]
      provision_actions = []
      manage_actions    = ["rds:*"]
      tag_actions       = ["rds:AddTagsToResource"]
      untag_actions     = ["rds:RemoveTagsFromResource"]
    }
  }

  enabled_crossplane_capabilities = {
    for key, def in local.crossplane_capability_defs :
    key => def if contains(var.crossplane_capabilities, key)
  }

  # Reused by each role's inline policy and the shared permissions boundary.
  crossplane_statements = {
    for key, def in local.enabled_crossplane_capabilities : key => concat(
      length(def.observe_actions) > 0 ? [{
        Sid      = "${key}Observe"
        Effect   = "Allow"
        Action   = def.observe_actions
        Resource = ["*"]
      }] : [],
      length(def.create_actions) > 0 ? [{
        Sid      = "${key}CreateOwned"
        Effect   = "Allow"
        Action   = def.create_actions
        Resource = def.resource_arns
        Condition = {
          StringEquals = {
            "aws:RequestTag/crossplane-providerconfig" = "aws-${key}"
          }
        }
      }] : [],
      length(def.provision_actions) > 0 ? [{
        Sid      = "${key}Provision"
        Effect   = "Allow"
        Action   = def.provision_actions
        Resource = def.resource_arns
      }] : [],
      [{
        Sid      = "${key}ManageOwned"
        Effect   = "Allow"
        Action   = def.manage_actions
        Resource = def.resource_arns
        Condition = {
          StringEquals = {
            "aws:ResourceTag/crossplane-providerconfig" = "aws-${key}"
          }
        }
      }],
      length(def.tag_actions) > 0 ? [
        {
          # Preserve another provider's existing ownership tag.
          Sid      = "${key}DenyClaimTagged"
          Effect   = "Deny"
          Action   = def.tag_actions
          Resource = def.resource_arns
          Condition = {
            StringNotEquals = {
              "aws:ResourceTag/crossplane-providerconfig" = "aws-${key}"
            }
            Null = {
              "aws:ResourceTag/crossplane-providerconfig" = "false"
            }
          }
        },
        {
          Sid      = "${key}DenyChangeOwnership"
          Effect   = "Deny"
          Action   = def.tag_actions
          Resource = def.resource_arns
          Condition = {
            StringEquals = {
              "aws:ResourceTag/crossplane-providerconfig" = "aws-${key}"
            }
            StringNotEquals = {
              "aws:RequestTag/crossplane-providerconfig" = "aws-${key}"
            }
            Null = {
              "aws:RequestTag/crossplane-providerconfig" = "false"
            }
          }
        },
        {
          Sid      = "${key}DenyRemoveOwnership"
          Effect   = "Deny"
          Action   = def.untag_actions
          Resource = def.resource_arns
          Condition = {
            "ForAnyValue:StringEquals" = {
              "aws:TagKeys" = ["crossplane-providerconfig"]
            }
          }
        },
      ] : [],
    )
  }

  # Boundary ceiling: enabled capabilities only, with no IAM access.
  crossplane_boundary_statements = concat(
    flatten([for stmts in values(local.crossplane_statements) : stmts]),
    [{ Sid = "DenyIAMEscalation", Effect = "Deny", Action = ["iam:*"], Resource = ["*"] }],
  )

  any_crossplane_capability = length(local.enabled_crossplane_capabilities) > 0

  # AWS permits one boundary per role; an organization boundary takes precedence.
  # The per-role inline policy still enforces capability and ownership scoping.
  org_boundary_set        = var.iam_role_permissions_boundary != null && var.iam_role_permissions_boundary != ""
  crossplane_boundary_arn = local.org_boundary_set ? var.iam_role_permissions_boundary : (local.any_crossplane_capability ? aws_iam_policy.crossplane_boundary[0].arn : null)
}

# Local boundary when the organization does not mandate one.
resource "aws_iam_policy" "crossplane_boundary" {
  count       = local.any_crossplane_capability && !local.org_boundary_set ? 1 : 0
  name        = "${local.name_prefix}-boundary"
  description = "Permissions boundary (upper bound) for Crossplane provider roles"
  tags        = var.tags
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.crossplane_boundary_statements
  })
}

data "aws_iam_policy_document" "crossplane_provider_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession"]
    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "crossplane_provider" {
  for_each             = local.enabled_crossplane_capabilities
  name                 = "${local.name_prefix}-${each.key}"
  assume_role_policy   = data.aws_iam_policy_document.crossplane_provider_trust.json
  permissions_boundary = local.crossplane_boundary_arn
  tags                 = var.tags
}

resource "aws_iam_role_policy" "crossplane_provider" {
  for_each = local.enabled_crossplane_capabilities
  name     = "${each.key}-provisioner"
  role     = aws_iam_role.crossplane_provider[each.key].id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.crossplane_statements[each.key]
  })
}

# Bind each capability role to its provider service account.
resource "aws_eks_pod_identity_association" "crossplane_provider" {
  for_each        = local.enabled_crossplane_capabilities
  cluster_name    = module.eks_cluster.cluster_name
  namespace       = local.crossplane_namespace
  service_account = each.value.service_account
  role_arn        = aws_iam_role.crossplane_provider[each.key].arn
  tags            = var.tags
}

output "crossplane_provider_role_arns" {
  description = "IAM roles assumed by Crossplane providers via Pod Identity, keyed by capability"
  value       = { for key, role in aws_iam_role.crossplane_provider : key => role.arn }
}
