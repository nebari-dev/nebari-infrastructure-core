# Opt-in AWS permissions for application infrastructure provisioned through
# Crossplane. Every provider controller has its own EKS Pod Identity role,
# inline policy, and (unless an organization boundary is supplied) permissions
# boundary. No static AWS credentials are stored in the cluster.
#
# Enabling the S3 capability also enables dedicated IAM and EKS providers. S3
# creates the bucket; IAM creates one bounded runtime role per ObjectStore; EKS
# binds that role to the requesting Kubernetes ServiceAccount. The S3 provider
# never receives IAM or EKS permissions.
#
# Cloud resources use the tool-neutral `${project_name}-apps-*` lifecycle
# namespace. Mutations and deletion additionally require the provider's
# `crossplane-providerconfig` ownership tag. Tags are not immutable creation
# provenance, so reserve the apps prefix and prevent workload users from
# adopting resources through `crossplane.io/external-name`.
#
# S3 was verified with provider-aws v2.6.1. IAM and EKS permissions must be
# verified end-to-end with the ObjectStore Composition before production use.
# RDS remains unverified and may need tightly scoped EC2 or KMS permissions.

locals {
  crossplane_namespace = "crossplane-system"
  # Stable lifecycle namespace independent of the provisioning tool.
  name_prefix = "${var.project_name}-apps"

  # Workload roles exist under a dedicated IAM path and name prefix. PassRole
  # uses this ARN rather than tags because AWS does not recommend ResourceTag
  # conditions for iam:PassRole.
  crossplane_workload_role_path = "/nebari/${var.project_name}/workloads/"
  crossplane_workload_role_arn  = "arn:aws:iam::*:role${local.crossplane_workload_role_path}${local.name_prefix}-*"

  object_store_identity_enabled = contains(var.crossplane_capabilities, "s3")
  org_boundary_set              = var.iam_role_permissions_boundary != null && var.iam_role_permissions_boundary != ""

  # service_account and the aws-<key> ProviderConfig name form the Pod Identity
  # and ownership-tag contracts with the GitOps manifests.
  crossplane_resource_capability_defs = {
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

  enabled_crossplane_resource_capabilities = {
    for key, def in local.crossplane_resource_capability_defs :
    key => def if contains(var.crossplane_capabilities, key)
  }

  # IAM and EKS are internal dependencies of managed object storage, not
  # additional user-facing opt-ins.
  crossplane_provider_service_accounts = merge(
    {
      for key, def in local.enabled_crossplane_resource_capabilities :
      key => def.service_account
    },
    local.object_store_identity_enabled ? {
      iam = "provider-aws-iam"
      eks = "provider-aws-eks"
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

  # Resource-provider policies use a common create/manage/ownership shape.
  crossplane_resource_statements = {
    for key, def in local.enabled_crossplane_resource_capabilities : key => concat(
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
        {
          Sid      = "${key}DenyIAMEscalation"
          Effect   = "Deny"
          Action   = ["iam:*"]
          Resource = ["*"]
        },
      ] : [],
    )
  }

  # The IAM provider may create and reconcile only bounded workload roles. It
  # cannot create users, access keys, instance profiles, or managed policies.
  crossplane_iam_statements = [
    {
      Sid    = "iamObserveWorkloadRoles"
      Effect = "Allow"
      Action = [
        "iam:GetRole",
        "iam:GetRolePolicy",
        "iam:ListAttachedRolePolicies",
        "iam:ListInstanceProfilesForRole",
        "iam:ListRolePolicies",
        "iam:ListRoleTags",
      ]
      Resource = [local.crossplane_workload_role_arn]
    },
    {
      Sid      = "iamCreateBoundedWorkloadRoles"
      Effect   = "Allow"
      Action   = ["iam:CreateRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringEquals = {
          "aws:RequestTag/crossplane-providerconfig" = "aws-iam"
          "iam:PermissionsBoundary"                  = local.crossplane_workload_boundary_arn
        }
      }
    },
    {
      # TagRole is a dependent create action. Name-prefix scoping plus the
      # request tag permits initial ownership; explicit denies below prevent
      # taking over a differently owned role or changing the reserved tag.
      Sid      = "iamTagWorkloadRoles"
      Effect   = "Allow"
      Action   = ["iam:TagRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringEquals = {
          "aws:RequestTag/crossplane-providerconfig" = "aws-iam"
        }
      }
    },
    {
      Sid    = "iamManageOwnedWorkloadRoles"
      Effect = "Allow"
      Action = [
        "iam:DeleteRole",
        "iam:DeleteRolePolicy",
        "iam:PutRolePolicy",
        "iam:UntagRole",
        "iam:UpdateAssumeRolePolicy",
      ]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringEquals = {
          "iam:ResourceTag/crossplane-providerconfig" = "aws-iam"
        }
      }
    },
    {
      Sid      = "iamRestoreRequiredBoundary"
      Effect   = "Allow"
      Action   = ["iam:PutRolePermissionsBoundary"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringEquals = {
          "iam:PermissionsBoundary"                   = local.crossplane_workload_boundary_arn
          "iam:ResourceTag/crossplane-providerconfig" = "aws-iam"
        }
      }
    },
    {
      Sid      = "iamDenyBoundaryRemoval"
      Effect   = "Deny"
      Action   = ["iam:DeleteRolePermissionsBoundary"]
      Resource = [local.crossplane_workload_role_arn]
    },
    {
      Sid      = "iamDenyClaimTagged"
      Effect   = "Deny"
      Action   = ["iam:TagRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringNotEquals = {
          "iam:ResourceTag/crossplane-providerconfig" = "aws-iam"
        }
        Null = {
          "iam:ResourceTag/crossplane-providerconfig" = "false"
        }
      }
    },
    {
      Sid      = "iamDenyChangeOwnership"
      Effect   = "Deny"
      Action   = ["iam:TagRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringEquals = {
          "iam:ResourceTag/crossplane-providerconfig" = "aws-iam"
        }
        StringNotEquals = {
          "aws:RequestTag/crossplane-providerconfig" = "aws-iam"
        }
        Null = {
          "aws:RequestTag/crossplane-providerconfig" = "false"
        }
      }
    },
    {
      Sid      = "iamDenyRemoveOwnership"
      Effect   = "Deny"
      Action   = ["iam:UntagRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        "ForAnyValue:StringEquals" = {
          "aws:TagKeys" = ["crossplane-providerconfig"]
        }
      }
    },
  ]

  # The EKS provider may manage Pod Identity associations only on this cluster.
  # Namespace and ServiceAccount are not IAM condition keys for the create API;
  # the platform Composition and generated workload-role trust policy must bind
  # those exact values.
  crossplane_eks_statements = [
    {
      Sid      = "eksObserveClusterAssociations"
      Effect   = "Allow"
      Action   = ["eks:DescribeCluster", "eks:ListPodIdentityAssociations"]
      Resource = [module.eks_cluster.cluster_arn]
    },
    {
      Sid      = "eksCreateOwnedAssociations"
      Effect   = "Allow"
      Action   = ["eks:CreatePodIdentityAssociation"]
      Resource = [module.eks_cluster.cluster_arn]
      Condition = {
        StringEquals = {
          "aws:RequestTag/crossplane-providerconfig" = "aws-eks"
        }
      }
    },
    {
      Sid    = "eksObserveOwnedAssociations"
      Effect = "Allow"
      Action = [
        "eks:DescribePodIdentityAssociation",
        "eks:ListTagsForResource",
      ]
      Resource = [
        "arn:aws:eks:${var.region}:*:podidentityassociation/${module.eks_cluster.cluster_name}/*",
      ]
    },
    {
      Sid    = "eksManageOwnedAssociations"
      Effect = "Allow"
      Action = [
        "eks:DeletePodIdentityAssociation",
        "eks:TagResource",
        "eks:UntagResource",
        "eks:UpdatePodIdentityAssociation",
      ]
      Resource = [
        "arn:aws:eks:${var.region}:*:podidentityassociation/${module.eks_cluster.cluster_name}/*",
      ]
      Condition = {
        StringEquals = {
          "aws:ResourceTag/crossplane-providerconfig" = "aws-eks"
        }
      }
    },
    {
      Sid      = "eksPassOnlyWorkloadRoles"
      Effect   = "Allow"
      Action   = ["iam:PassRole"]
      Resource = [local.crossplane_workload_role_arn]
      Condition = {
        StringEquals = {
          "iam:PassedToService" = "pods.eks.amazonaws.com"
        }
      }
    },
    {
      # PassRole is the EKS provider's only IAM permission.
      Sid    = "eksDenyIAMMutation"
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
      Resource = ["*"]
    },
  ]

  crossplane_provider_statements = merge(
    local.crossplane_resource_statements,
    {
      for key, statements in {
        iam = local.crossplane_iam_statements
        eks = local.crossplane_eks_statements
      } : key => statements if local.object_store_identity_enabled
    },
  )
}

# One local boundary per provider prevents permission bleed between capabilities.
# When an organization boundary is configured it occupies the single available
# boundary slot; the identical per-role inline policy still enforces this scope.
resource "aws_iam_policy" "crossplane_provider_boundary" {
  for_each = local.org_boundary_set ? {} : local.crossplane_provider_statements

  name        = "${local.name_prefix}-${each.key}-provider-boundary"
  description = "Permissions boundary for the Crossplane AWS ${each.key} provider"
  tags        = var.tags
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = each.value
  })
}

# Provider roles are usable only by their exact ServiceAccount in this cluster.
data "aws_iam_policy_document" "crossplane_provider_trust" {
  for_each = local.crossplane_provider_service_accounts

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
    condition {
      test     = "StringEquals"
      variable = "aws:RequestTag/kubernetes-service-account"
      values   = [each.value]
    }
  }
}

resource "aws_iam_role" "crossplane_provider" {
  for_each = local.crossplane_provider_service_accounts

  name               = "${local.name_prefix}-${each.key}"
  assume_role_policy = data.aws_iam_policy_document.crossplane_provider_trust[each.key].json
  permissions_boundary = local.org_boundary_set ? (
    var.iam_role_permissions_boundary
  ) : aws_iam_policy.crossplane_provider_boundary[each.key].arn
  tags = var.tags
}

resource "aws_iam_role_policy" "crossplane_provider" {
  for_each = local.crossplane_provider_service_accounts

  name = "${each.key}-provisioner"
  role = aws_iam_role.crossplane_provider[each.key].id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.crossplane_provider_statements[each.key]
  })
}

# Bind each provider role to its exact controller ServiceAccount.
resource "aws_eks_pod_identity_association" "crossplane_provider" {
  for_each = local.crossplane_provider_service_accounts

  cluster_name    = module.eks_cluster.cluster_name
  namespace       = local.crossplane_namespace
  service_account = each.value
  role_arn        = aws_iam_role.crossplane_provider[each.key].arn
  tags            = var.tags
}

output "crossplane_provider_role_arns" {
  description = "IAM roles assumed by Crossplane providers via Pod Identity, keyed by provider"
  value       = { for key, role in aws_iam_role.crossplane_provider : key => role.arn }
}

output "crossplane_workload_boundary_arn" {
  description = "Required permissions boundary for Crossplane-created workload roles"
  value       = local.crossplane_workload_boundary_arn
}

output "crossplane_workload_role_path" {
  description = "Required IAM path for Crossplane-created workload roles"
  value       = local.object_store_identity_enabled ? local.crossplane_workload_role_path : null
}
