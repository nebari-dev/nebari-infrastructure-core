---
promise: P8
level: stable
state: built
conformance_test: pending
changes:
  - date: 2026-11-17
    kind: founding
    hash: c66b85ce29233ae0
---
# P8: Argo CD project for packs

## Promise

Pack Applications use a project that is allowed to deploy them. Twelve example Applications use the deny-all `default` project today.

## Level

stable

## State

built. The AppProject `nebari-apps` exists today. It allows more than this page promises; packs stay within what is promised here, because the rest may be narrowed.

## Interface

AppProject `nebari-apps`. Pack Applications set `project: nebari-apps`. The project accepts the pack's source repository and lets it deploy any namespaced kind into its own namespaces, plus these cluster-scoped kinds: `Namespace`, `CustomResourceDefinition`, `ClusterRole`, `ClusterRoleBinding`, `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration`. These are the cluster-scoped kinds packs ship today, except `StorageClass`, which only data-science-pack ships for its own NFS server and which the contract does not promise because it does not offer RWX.

## Packs may rely on

- Applications with `project: nebari-apps` syncing.
- Any namespaced kind in the pack's own namespaces.
- The six cluster-scoped kinds listed above.

## Packs must not rely on

- The project accepting every repository or every namespace (#536 and ADR-0010 may narrow it).
- Any other cluster-scoped kind. The project allows them today but may stop.
- Write access to the GitOps repository.
- The `default` project. An Application in `default` fails to sync. The reference will quote the exact sync error once the conformance test captures it.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference Application in `nebari-apps` syncs on every target, and an Application in `default` fails to sync.
