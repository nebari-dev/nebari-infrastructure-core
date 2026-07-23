# Day-0 IAM trust anchors for Crossplane software-pack provisioning (ADR-0012).
# Scoped, secret-less provider roles assumed via EKS Pod Identity — no static AWS
# keys ever land in-cluster (Security Req #4). Each capability gets its OWN role
# (Req #5) under a permissions boundary as a hard ceiling (Req #6).
#
# Capabilities are opt-in per cluster via `crossplane_capabilities`. Everything
# here is a for_each over the enabled set, so nothing is provisioned unless a
# capability is explicitly requested. Add a capability by extending
# local.crossplane_capability_defs plus the matching gitops manifests
# (provider-aws-<key>.yaml, configs/aws-<key>/) — no other HCL changes.
#
# SCOPING MODEL (see below): application infrastructure lives in the neutral
# `${project_name}-apps-*` lifecycle namespace. Foundational OpenTofu resources
# MUST NOT use that prefix. Within the namespace, the role can only mutate/delete
# resources carrying its `crossplane-providerconfig` ownership tag, which upjet
# stamps on managed resources automatically.
#
# This is lifecycle-domain isolation, not immutable creation provenance: AWS tag
# APIs cannot distinguish tag-on-create from a later claim of an untagged
# resource. Reserve the apps prefix for this control plane (through organization
# policy where other principals can create resources) and prevent workload users
# from supplying `crossplane.io/external-name` through admission policy or a
# constrained Composition API. Pack-to-pack isolation is intentionally NOT
# enforced here — all packs share a capability's ProviderConfig/role (same-admin
# trust model); that boundary lives in Crossplane RBAC/namespaces.
#
# NOTE: the per-capability action lists must be verified against provider-aws v2
# behavior in-cluster (Describe calls that need Resource:"*", the create->readback
# bootstrap order per service). S3 was verified end-to-end on 2026-07-22 (a raw
# Bucket reconcile plus iam simulate-principal-policy) — its provision tier now
# carries the create-time reads that would otherwise deadlock. RDS is still a
# first cut (unverified; also needs supporting ec2/kms grants + an iam:PassRole
# decision). The lists are structured so tuning means editing actions, not shape.

locals {
  crossplane_namespace = "crossplane-system"
  # Tool-neutral lifecycle namespace shared by the IAM resources and the cloud
  # resources they are allowed to manage. This remains stable if Crossplane is
  # replaced by another application-infrastructure controller.
  name_prefix = "${var.project_name}-apps"

  # Per-capability provisioner definitions. service_account is part of the EKS
  # Pod Identity trust contract and MUST match the DeploymentRuntimeConfig service
  # account in manifests/crossplane/providers/provider-aws-<key>.yaml. The tag
  # value equals the ProviderConfig name (configs/aws-<key>/provider-config.yaml),
  # which is what upjet writes into `crossplane-providerconfig`.
  crossplane_capability_defs = {
    s3 = {
      service_account = "provider-aws-s3"
      resource_arns = [
        "arn:aws:s3:::${local.name_prefix}-*",
      ]
      # Account-level discovery that cannot be resource-scoped (ListAllMyBuckets)
      # plus GetBucketLocation (harmless, returns a region) so the provider can
      # locate buckets during its reconcile sweep.
      observe_actions = ["s3:ListAllMyBuckets", "s3:GetBucketLocation"]
      # Modern S3 supports tags in CreateBucket. provider-aws-s3 v2 uses that path
      # when TagResource is allowed, so creation can require the ownership tag in
      # the request instead of granting the legacy, takeover-prone
      # PutBucketTagging action on every name-matching bucket.
      create_actions = ["s3:CreateBucket", "s3:TagResource"]
      # The upjet provider reads the new bucket's sub-configs
      # (GetAccelerateConfiguration, GetLifecycleConfiguration, …) immediately
      # after create to populate state. Those reads happen before ABAC is enabled,
      # so they remain name-prefix-scoped but un-tag-gated.
      #
      # Bucket ABAC is disabled by default. aws:ResourceTag conditions are not
      # evaluated until it is enabled, so s3:PutBucketAbac must also bootstrap
      # without the ownership-tag condition. Workload compositions should create
      # a BucketAbac whose managementPolicies omit Delete so deleting that helper
      # does not disable ABAC before the Bucket controller calls DeleteBucket.
      # Verified in-cluster with provider-aws-s3 v2.6.1 on 2026-07-23.
      # Reads stay name-prefix-scoped (NOT account-wide); mutate/delete stay gated.
      # Only bucket ARNs are in resource_arns, so these wildcards cannot authorize
      # GetObject or other object-ARN data access. ListBucket remains available for
      # the provider's bucket reconcile and can reveal object names, but not data.
      provision_actions = ["s3:PutBucketAbac", "s3:Get*", "s3:List*"]
      # Mutate/delete on owned buckets only, gated by the ownership tag.
      manage_actions = ["s3:*"]
      # Once present, the ownership tag is reserved. These actions are denied
      # separately below when they would claim a differently-owned bucket, change
      # this ownership value, or remove the ownership key.
      tag_actions   = ["s3:TagResource"]
      untag_actions = ["s3:UntagResource"]
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
      # RDS Describe* largely cannot be resource-scoped, so reads stay at "*"
      # (unlike S3, which name-prefix-scopes reads in its provision tier).
      observe_actions = ["rds:Describe*", "rds:ListTagsForResource"]
      # RDS create APIs accept tags and require AddTagsToResource as a dependent
      # action. Keep all of them in CreateOwned so the ownership request tag is
      # mandatory; there is no untagged RDS provisioning tier.
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

  # Intersection of what the operator enabled and what we know how to provision.
  # Unknown keys are rejected at config validation, so this is belt-and-suspenders.
  enabled_crossplane_capabilities = {
    for key, def in local.crossplane_capability_defs :
    key => def if contains(var.crossplane_capabilities, key)
  }

  # The statement groups for one capability, reused by the per-role inline
  # policy and the shared boundary. Sids are key-suffixed so the union in the
  # boundary has no Sid collisions. Observe is read-only at "*"; Create requires
  # the ownership request tag; Provision contains the narrow operations that must
  # run before bucket ABAC can evaluate resource tags; Manage requires both the
  # ARN prefix and the ownership tag.
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
          # Do not let the role replace another owner's reserved tag. The Null
          # check limits this Deny to resources where the key already exists;
          # tag-on-create has no existing resource tag and remains possible.
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

  # The shared boundary ceiling: the union of every enabled capability's Allows
  # plus a hard Deny on IAM (no privilege escalation, no self-granting roles).
  crossplane_boundary_statements = concat(
    flatten([for stmts in values(local.crossplane_statements) : stmts]),
    [{ Sid = "DenyIAMEscalation", Effect = "Deny", Action = ["iam:*"], Resource = ["*"] }],
  )

  any_crossplane_capability = length(local.enabled_crossplane_capabilities) > 0

  # Many orgs enforce a specific permissions-boundary ARN on every role via SCP.
  # When one is provided, defer to it (AWS allows only one boundary per role, and
  # the SCP would reject ours); the inline policies below still enforce the
  # Crossplane scoping. Otherwise attach our own tag-scoped ceiling.
  org_boundary_set        = var.iam_role_permissions_boundary != null && var.iam_role_permissions_boundary != ""
  crossplane_boundary_arn = local.org_boundary_set ? var.iam_role_permissions_boundary : (local.any_crossplane_capability ? aws_iam_policy.crossplane_boundary[0].arn : null)
}

# Our own permissions boundary — created only when capabilities are enabled and
# the org did not mandate its own.
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

# Provisioner roles are trusted only by the EKS Pod Identity service principal.
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

# Pod Identity association: binds each role to its provider SA. Secret-less —
# the pod gets creds from the Pod Identity Agent (verified ACTIVE in Phase 0).
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
