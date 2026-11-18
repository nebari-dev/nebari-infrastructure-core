# Compatibility

This page is the promise Nebari makes to pack authors about change. It follows the [Go 1 compatibility promise](https://go.dev/doc/go1compat): a pack written against the contract keeps working, unchanged, as Nebari evolves.

## The promise

A pack that uses only stable promises, and works on a cluster implementing some contract version, keeps working, unchanged, on every cluster implementing a later version. This holds before 1.0 as well: a stable promise is covered from the contract version in which it is built.

A stable promise that is partly built is covered for the parts that are built. Its page lists what is still missing, and those parts are covered once they are built.

"Keeps working" means the interface the promise names (a CRD field, a ConfigMap key, a label, a class, a claim, an endpoint) keeps its meaning, and the behavior the promise page describes keeps holding on every supported target. It does not mean that anything outside the promise stays the same.

Nothing in a built stable promise is removed or changed incompatibly, in any later contract version, except under the exceptions below. Deprecated parts stay, in their deprecated form (see [Deprecation](deprecation.md)).

## What is not covered

- **Planned promises.** A promise whose state is `planned` carries no compatibility promise until it is built. Its intended interface may change before then.
- **Experimental promises.** Experimental promises may change or be removed in any contract version, without notice (see [Deprecation](deprecation.md#changing-or-removing-experimental-promises)). Packs that use them do so at their own risk and say so in their README.
- **Anything not in the contract.** Names, namespaces, Services, chart-generated resources and everything on the [do-not-rely-on list](../do-not-rely-on.md) can change in any release. Something that exists today is not promised unless a promise page says so.
- **Provided but not promised.** Capabilities on the [provided-not-promised list](../provided-not-promised.md) can change or disappear on any target.
- **Performance and capacity.** Request latency, start-up time, default resource sizes and node counts may change between releases. They are not correctness.
- **Tooling around the cluster.** `nic` command output, log formats and the layout of the GitOps repository are not part of the pack contract.

## Exceptions

As with Go 1, the promise is not absolute. These are the only reasons a built stable promise may change incompatibly:

- **Security.** A vulnerability in a promise, or in how Nebari implements it, may need a fix that breaks compatibility. The [security policy](security.md) governs these changes.
- **Unspecified behavior.** A pack that depends on behavior the promise page does not describe may break when that behavior changes. If a promise page is silent on something, it is not promised.
- **Errors in the contract.** If a promise page is inconsistent or incomplete, fixing it may change what a pack can rely on. The fix goes through a platform RFD (see [Proposals and governance](proposals.md)) and ships with a [compatibility setting](deprecation.md#behavior-changes-and-compatibility-settings) where one is possible.
- **Bugs.** If Nebari violates a promise, a pack that depends on the violation may break when the bug is fixed.
- **Upstream end of life.** If an upstream project a promise depends on (for example CloudNativePG or a Kubernetes API) ends support for an interface, Nebari follows the [Deprecation](deprecation.md) process with the longest window upstream allows, and says so on the promise page.

Every exception that changes a stable promise is announced in the release notes of the contract version that carries it, with the reason and what a pack must do, and bumps the version by the effort it costs (see [Versioning](versioning.md)).

## How compatibility is checked

Every stable promise gets a conformance test that runs on every supported target (see [Conformance](conformance.md); the suite is planned). Once it exists, a change that turns a conformance test red for an existing promise is a compatibility break and is not merged unless it falls under an exception above, and the suite for each contract version is kept and run against later releases.
