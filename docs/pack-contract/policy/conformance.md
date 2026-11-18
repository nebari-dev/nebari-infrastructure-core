# Conformance

This page says how a cluster and a pack show they meet the contract, and what each side must do to keep the [compatibility promise](compatibility.md) working for it.

## States and conformance

A promise is **built** when it exists on every supported target as its page describes. It is **conformance-tested** when its test passes on every supported target. Contract 1.0 requires every required promise to be both. Until the conformance suite exists, `built` rests on the behavior NIC ships today, and each page says what its test will prove.

## The conformance suite

State: planned. Tracking issue: to be filed when the contract is accepted.

The suite will be a `pack_contract` group in NIC's user journey tests ([ADR-0017](../../adr/0017-user-journey-tests-for-foundational-software.md)). It installs a reference pack that uses every stable promise, and every built experimental promise, through contract interfaces only, and checks each promise end to end.

- Each promise page names its test.
- The suite for each contract version is kept and runs against later releases, so a release cannot silently break an older promise.
- A generic subset checks any pack chart (its Application syncs under `nebari-apps`, its routes answer behind SSO), so pack repositories can run it in their own CI.

## Cluster conformance

**NIC-managed clusters.** Once the suite exists, every NIC release runs it on every supported target before it ships, and the contract version a release implements is the version whose suite passed. Today the user journey tests run on release and on demand.

**Existing clusters.** NIC does not control the CNI or storage on a cluster it did not create. A preflight in `nic deploy` (planned) will check the requirements it can observe before installing anything: exactly one default StorageClass, the CloudNativePG CRDs absent or in the supported range, and NetworkPolicy enforcement once P16 is built. It stops with a message that names the missing requirement, and runs on every provider, so a NIC-managed cluster that drifts is caught too. Admins can run the suite against their cluster to check the rest.

**Other distributions.** A platform that is not deployed by NIC may say it conforms to contract `X.Y.Z` when that version's suite passes on it and the results are published. A claim names the experimental promises it covers separately.

## What a pack must do

A pack stays inside the compatibility promise when it:

- uses only interfaces that a promise page names, and nothing on the [do-not-rely-on list](../do-not-rely-on.md);
- registers its Applications in the `nebari-apps` project;
- declares the contract version it targets (see [Pack-declared contract version](versioning.md#pack-declared-contract-version));
- lists every promise it uses in its README, marking the experimental ones;
- tolerates keys and fields it does not know (for example new keys in `nebari-platform`), because Micro versions add them;
- works, or fails with a clear message, where a [provided-not-promised](../provided-not-promised.md) capability is missing;
- keeps a security contact in its `SECURITY.md`, so it can receive PRIVATE-track details of [break-glass](security.md) changes under embargo.

A pack that depends on something outside the contract may break in any release, and that breakage is not a compatibility violation.

Packs generated from `nebari-dev/software-pack-template` will start out meeting these rules once the template is aligned with the contract, and the template will run the generic conformance subset in its CI.
