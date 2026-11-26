# Versioning and support

This page defines what a contract version number means, how it relates to NIC and nebari-operator releases, how long each version gets fixes, and how a pack says which version it needs.

## Version numbers

The contract is versioned with [EffVer](https://jacobtomlinson.dev/effver/), the convention used across nebari-dev and by the [Pack Specification](https://github.com/nebari-dev/nebari-operator/blob/main/docs/pack-specification.md). A version measures the effort an upgrade costs a pack author, not how much changed in Nebari.

| Segment | Means for a pack author | Typical changes |
|---|---|---|
| Micro | Nothing to do | new promises, new keys or fields, graduations from experimental to stable, deprecation notices, corrections that change no promise, conformance test fixes |
| Meso | Some work for some packs | an experimental promise changes shape; a new recommendation packs should adopt |
| Macro | Real work | a stable promise is removed or changed incompatibly |

While the contract is pre-1.0, EffVer collapses to `0.Macro.Micro`: any change that costs a pack author work bumps Macro. 1.0 is declared when every required promise is built and its conformance test passes on every supported target. After 1.0, a Macro bump would mean a stable promise broke outside the compatibility exceptions, and there is no plan for one.

The [compatibility promise](compatibility.md) is what keeps Macro bumps rare: built stable promises do not change incompatibly except for the listed exceptions, whatever the version number. A [security exception](security.md) is versioned as a Micro change, even when it costs some packs work, so that fixes can ship on every supported line without colliding with newer version numbers; the advisory says what packs must do.

## Contract versions and releases

- Each contract version names the Pack Specification version it includes, so the two cannot disagree. Contract `0.1.0` includes Pack Specification `0.1.0`, plus the relaxation of the one-NebariApp rule described on [P2](../p02-several-nebariapps.md) once that specification change is accepted.
- Each NIC release implements exactly one contract version. Its release notes name it, and packs receive it as `global.nebari.contractVersion` in the [platform values](../p05-platform-facts.md) once P5 is built, on full installations only.
- Each nebari-operator release states the range of contract versions it supports. A NIC release pins an operator version inside that range.
- A NIC release may implement a newer contract version than the previous NIC release, never an older one.

## Support window

A **release line** is all contract versions that share their leading segments: `0.Macro` before 1.0, and `Macro.Meso` from 1.0. Following [Go's release policy](https://go.dev/doc/devel/release#policy), a line is supported until two newer lines exist, and in any case for at least 12 months after the next line is released. The newest line is always supported. Because new promises and other growth are Micro changes, they extend the current line rather than starting a new one.

While a line is supported, the newest NIC release that implements the newest version in that line gets patch releases for critical problems and security fixes. Unsupported versions get no fixes; clusters move to a supported NIC release to receive them.

Packs should test against the oldest supported line they claim and the newest one.

## Compatibility matrix

The matrix is updated with every release that changes a contract version or a supported range.

| Contract | Pack Specification | NIC releases | nebari-operator releases | Status |
|---|---|---|---|---|
| 0.1.0 | 0.1.0 (one-NebariApp rule relaxation pending) | not yet released | not yet released | proposed |

## Pack-declared contract version

Packs already declare the Pack Specification version they target in `pack-metadata.yaml`, as the [pack policy](https://github.com/nebari-dev/governance/blob/main/pack-policy.md#maturity-and-release-readiness) requires. Packs will declare the contract version they target in the same file, the way a Go module declares its `go` version. Because each contract version names the Pack Specification version it includes, the two declarations stay consistent. The field name belongs to the `pack-metadata.yaml` schema in `nebari-dev/software-pack-template`.

State: planned. How a cluster learns a pack's declared version, so that it can act on it, is fixed in its own design. Once built, the declaration does two things:

- **Refusal with a reason.** A pack that declares a newer contract version than the cluster implements is refused with a message naming both versions, instead of failing later in an obscure way.
- **Behavior defaults.** When a stable behavior changes under a [compatibility setting](deprecation.md#behavior-changes-and-compatibility-settings), a pack that declares an older version keeps the old behavior by default, the way a Go module with an older `go` line keeps older GODEBUG defaults.

Until it is built, packs state their target contract version in their README as well, and compatibility settings default per cluster as described on the [Deprecation](deprecation.md) page.
