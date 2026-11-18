# Security exceptions (break glass)

The [compatibility promise](compatibility.md) gives way when keeping a promise would leave clusters exposed. This page says when Nebari may break a promise for security, how fast, and how pack authors find out. It adapts the tracks in [Go's security policy](https://go.dev/doc/security/policy).

## When break glass applies

Experimental promises may change at any time (see [Deprecation](deprecation.md#changing-or-removing-experimental-promises)), so a security fix to one ships as an ordinary change, through the tracks below. Nebari may change or remove a stable promise outside the normal [RFD process](proposals.md) and [deprecation](deprecation.md) rules only when:

- the promise as specified, or as implemented, lets an attacker cross a boundary the platform is meant to hold (between packs, between users, or between a pack and the platform); and
- no fix that keeps the promise intact closes the hole.

If the hole can be closed without breaking the promise, it is fixed that way, and this page does not apply.

## Rules for a break-glass change

- **Smallest change.** The fix changes only what the vulnerability requires. Anything more goes through the normal process.
- **New behavior for everyone.** As with Go's security GODEBUGs, the fixed behavior applies to every pack regardless of the contract version it declares.
- **Escape hatch only with consent.** A [compatibility setting](deprecation.md#behavior-changes-and-compatibility-settings) that restores the old behavior is added only when an admin can make an informed choice to accept the risk; its page states the risk plainly. When restoring the old behavior would expose other tenants, there is no setting.
- **Supported lines.** Fixes ship in patch releases of the NIC releases that implement every [supported line](versioning.md#support-window). Each fixed line gets a new Micro contract version under the security exception (see [Versioning](versioning.md)), and the advisory says what packs must do.
- **RFD after the fact.** Within 30 days of the release, a platform RFD records the change, whether the promise needs a redesign, and what its page now says.

## Tracks

Reports come in through each repository's `SECURITY.md`. The maintainers handling a report assign it a track.

| Track | Covers | Fix and notice |
|---|---|---|
| PUBLIC | low-impact or already-public weaknesses, or niche configurations | fixed in the open; next scheduled patch release; release notes describe the change |
| PRIVATE | a violation of a promise's security properties | fixed in private; three to seven days before the release, a public notice says a security release is coming and which components it affects, without details; pack maintainers with a registered security contact who use the affected promise get the details under embargo at the same time; then the patch release and an advisory |
| URGENT | actively exploited, or a threat to every Nebari cluster | fixed in private; an immediate release, possibly without any advance notice |

Go pre-announces PRIVATE fixes publicly and shares no details ahead of the release. Telling affected pack maintainers the details under embargo is a deliberate departure: a break-glass change can require a pack to change too, and a pack that learns of it on release day leaves its users exposed or broken. The embargo ends at the release; pack maintainers who receive details do not disclose or ship fixes earlier.

## What the advisory says

Every break-glass change ships with a GitHub security advisory that states:

- the promise and the part of it that changed;
- the affected contract versions and the NIC and nebari-operator releases that fix it;
- what a pack must change, and how to tell whether a pack is affected;
- any compatibility setting, and the risk of using it.

Pack maintainers who want embargoed details register a security contact in their pack's `SECURITY.md` and list the promises they use, as [conformance](conformance.md#what-a-pack-must-do) requires.
