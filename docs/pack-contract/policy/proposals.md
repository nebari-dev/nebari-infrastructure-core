# Proposals and governance

This page says how a new capability is adopted into the contract, how an experimental promise graduates, and who decides. The contract is a platform contract under the [Nebari governance](https://github.com/nebari-dev/governance/issues/59): changes to it are platform RFDs, decided by Core, except changes to experimental promises, which need only a pull request. The process inside each RFD borrows from the [Go proposal process](https://github.com/golang/proposal#readme).

## When an RFD is needed

The governance requires a platform RFD for any change to behavior in NIC that packs rely on, and to the Pack Specification. For this contract that means:

- adding a promise, at either level;
- graduating an experimental promise to stable;
- deprecating a stable promise or any part of one;
- changing a stable behavior under a compatibility setting;
- adding a supported target, or dropping one;
- changing any policy in this reference.

These need only a pull request, because they change no promise: rationale, examples and links, conformance test fixes that do not change what a test proves, and per-target notes that describe existing behavior. Changing or removing an experimental promise also needs only a pull request, because experimental promises carry no stability guarantee (see [Deprecation](deprecation.md#changing-or-removing-experimental-promises)). Any change to a stable page's Promise, Level, Interface, may-rely or must-not-rely text is a change to the contract and needs an RFD. Security changes follow the [security policy](security.md) and get their RFD after the fix ships. If in doubt, file an RFD.

## The process

1. **RFD.** The author files a platform RFD in `nebari-dev/governance`. It names the capability, the packs that need it, the intended interface, how it works on every target, and how it meets the entry rules below. No full design is needed at this point.
2. **Discussion.** The RFD is open for comments. Discussion may ask for a design; if so, the author writes it in the repository that will own the change (a NIC ADR or design doc, or a nebari-operator design) and links it from the RFD.
3. **Decision.** Core decides by the vote the governance defines. A rejection says why and what would change the answer; "no" is temporary.
4. **Implementation.** Work proceeds in the owning repositories, each pull request linking the RFD. The promise page lands with state `planned`, then `built`.

[ADR-0023](../../adr/0023-pack-platform-contract.md), which proposes this contract and these policies, is itself accepted through a platform RFD.

## Entry rules

Core accepts a new promise only when all of these hold:

1. **Proven need:** at least two packs need it, or one pack plus a clear near-term need. One pack's need is that pack's to solve until a second pack needs it.
2. **Every target:** it can work on every supported target. Where a cloud lacks it, Nebari provides it in-cluster.
3. **Stable, implementation-neutral interface:** it is offered through a Kubernetes or open standard (a StorageClass, a NetworkPolicy, Gateway API, OIDC, OTLP) or through a Nebari-owned abstraction (a NebariApp field, a `nebari-platform` key, a named class or label), never through one product's API, and never through an incidental Service name, namespace or chart-generated resource. The aim is that any foundational software that satisfies the contract can be swapped in. A promise bound to one product is accepted only when no neutral interface exists yet: its front matter sets `bound_to`, and its Interface section says "Bound to <product>" and names a "Path to a neutral interface".
4. **Conformance-tested:** the RFD names the test that will prove it on every target.
5. **Versioned:** it fits the [versioning](versioning.md) rules, and its removal path is clear.

## Levels and graduation

New interfaces enter as **experimental**. Documenting behavior that already exists, unchanged, may enter as **stable** directly.

The first contract version is the one exception. Its levels are set by ADR-0023 itself: P2, P5 and P6 enter as stable even though they are not fully built, because they replace workarounds most packs carry today, and a pack author should be able to build on them as soon as they land.

An experimental promise graduates to stable through an RFD when:

- it has been built and shipped in at least one contract version;
- at least two packs use it in released versions;
- its conformance test has passed on every supported target for that time;
- no open RFD proposes changing its shape.

The clock for an experimental promise starts when it is built, not when it is listed. One that has not graduated within 18 months of being built is renewed by an RFD with a reason, or deprecated for removal (see [Deprecation](deprecation.md)).

## Who decides

Core decides every RFD, as the governance sets out, and records the decision on the RFD issue. Maintainers of NIC and nebari-operator review the designs and implementations in their repositories. Every accepted change links its RFD.

## How CI enforces these rules

Rules that people have to remember get lost, so CI checks them. Every promise page starts with YAML front matter that records its level, its state, the date an experimental promise was built, its conformance test, any renewals or deprecation, the product it is bound to, if any, and a list of changes to its protected text (Promise, Level, Interface, may-rely and must-not-rely). The Go test in `internal/packcontract` runs with every `go test ./...`, in CI and in the pre-commit hook, and fails when:

- a page's front matter, its Level and State sections, the [README](../README.md) table and the ADR-0023 table disagree, or a page is missing a section or has them out of order;
- the protected text changed without a new entry in `changes` carrying the hash the test prints. While ADR-0023 is Proposed, the founding entry's hash is updated with the text instead, because nothing has been promised yet;
- a change to a stable promise, or a graduation, has no link to a platform RFD in `nebari-dev/governance`;
- an incompatible change to a built stable promise names no [compatibility exception](compatibility.md#exceptions);
- a built experimental promise is past 18 months from its built date or latest renewal, and has not graduated or been deprecated;
- a promise marked `bound_to` does not say so in its Interface section, or names no path to a neutral interface (the test also lists every bound promise in its output);
- a built or partly built promise names no conformance test, or names one that does not exist, lives outside `tests/user_journeys/journeys/pack_contract/`, or skips on some targets. `pending` is accepted only before contract 1.0.

The check proves that the record is complete and consistent. It cannot prove that an RFD it links was accepted, that a change marked compatible really is, or that NIC behaves as a page says; reviewers and the conformance suite do that.
