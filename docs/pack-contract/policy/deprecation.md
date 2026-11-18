# Deprecation and behavior changes

This page says how a promise, or part of one, is retired, and how a stable behavior changes without breaking packs that depend on the old one. It follows Go's [deprecation convention](https://go.dev/wiki/Deprecated) and its [GODEBUG mechanism](https://go.dev/doc/godebug).

## Deprecating stable promises

Deprecation tells pack authors to move to something better. It never means removal while the compatibility promise holds: as in Go, a deprecated stable promise "will be preserved in its deprecated form to keep existing programs running."

A stable promise, field, key or value is deprecated by an accepted platform RFD (see [Proposals and governance](proposals.md)). When it is:

- **The promise page says so.** It gets a paragraph that begins with `Deprecated:`, names the contract version that deprecated it, and says what to use instead.
- **The release notes say so.** The contract version that deprecates something lists it with its replacement. A deprecation notice is a Micro change: nothing breaks.
- **Clusters say so at runtime (planned).** Where the interface allows it, using a deprecated part will be reported where the pack author or admin sees it: a `Deprecated` condition on the NebariApp that does not change `Ready`, a Kubernetes API warning for deprecated CRD fields or versions, and a warning from `nic deploy` listing the deprecated parts in use. Tracking issue: to be filed when the contract is accepted.

Removing a deprecated stable part happens only if Core accepts an RFD for it under the [compatibility exceptions](compatibility.md#exceptions). There is no plan to remove deprecated stable parts.

## Changing or removing experimental promises

Experimental promises may change shape or be removed, with notice:

- The change is announced as deprecated in one contract version and lands no earlier than 3 months later, in a later contract version.
- The promise page and release notes describe the change and the migration.
- An experimental promise has 18 months after it is built to graduate. Then it is graduated, renewed by an RFD with a reason, or deprecated for removal (see [Proposals and governance](proposals.md#levels-and-graduation)). Experiments do not linger.

## Behavior changes and compatibility settings

Sometimes a stable behavior has to change, because the old behavior was a bug, a security weakness, or an error in the contract (see the [exceptions](compatibility.md#exceptions)). When the change would break packs that depend on the old behavior, Nebari adds a compatibility setting that lets those packs opt back in to the old behavior, the way GODEBUG does for Go programs. State: planned; the first setting ships with the first behavior change that needs one.

- **Name and value.** Each setting has a name and documented values, listed on the promise page with the contract version that added it.
- **Cluster override.** An admin will set it per cluster in the NIC configuration (the exact key is fixed when the first setting is built), or set a default contract version to take that version's defaults for every setting.
- **Pack defaults.** Once [pack-declared contract versions](versioning.md#pack-declared-contract-version) are built, a pack that declares an older version gets the older default automatically. Until then, a cluster upgraded in place keeps the old behavior by default, and new clusters get the new behavior.
- **Visibility.** `nic deploy` will report every setting in use that is not the current default, so admins know what they will have to change.
- **Retention.** As in Go, a compatibility setting is kept for at least two years. Its page states when it will be removed, and removal follows the deprecation notice above.
- **Security exception.** Settings added for security changes follow the [security policy](security.md): the new behavior applies to every pack regardless of declared version, and a setting exists only where an admin can knowingly accept the risk.
