# ADR-0019: User Cleanup on Keycloak Deletion

## Status

Proposed (2026-10-07), DRAFT

Records the design behind the PoC in [nebari-operator#188](https://github.com/nebari-dev/nebari-operator/pull/188), discussed in [#668](https://github.com/nebari-dev/nebari-infrastructure-core/issues/668). The Keycloak side landed in [#673](https://github.com/nebari-dev/nebari-infrastructure-core/pull/673).

## Date

2026-10-07

## Context

Deleting a user in Keycloak removes their identity and nothing else. Every software pack that holds per-user state keeps it: JupyterHub home and workspace volumes, hub user records, team memberships, database rows. Each pack would have to notice the deletion on its own, and today none does.

Three things have to meet: Keycloak records the deletion, packs know what their per-user state is and how to remove it, and something has to connect the two at the right time. That something is the operator, which already holds Keycloak admin credentials and already watches pack resources.

The timing matters. Some cleanup should happen right away, stopping a running server so a deleted user cannot keep working. Some should wait, deleting storage only after a window in which a mistaken deletion can still be undone.

## Decision Drivers

- Packs own their cleanup logic. The platform cannot know what state a pack keeps or how to remove it.
- Cleanup must not run twice for the same user, including after an operator restart or a lost cursor.
- A pack's cleanup runs with the pack's permissions, not the operator's.
- Deletions must be visible: which users were handled, which hooks ran, which failed.
- The operator stays off the user-login path. Detection is a poll, not a webhook or a listener inside Keycloak.

## Considered Options

1. Poll Keycloak admin events and run pack-registered Jobs
2. Disable-and-mark: an admin disables the user and the platform performs the final Keycloak delete after the grace period
3. A Keycloak event listener SPI pushing deletions to the operator
4. Each pack polls Keycloak itself

## Decision Outcome

Poll Keycloak admin events and run pack-registered Jobs. It needs nothing deployed inside Keycloak, reuses the credentials and watch logic the operator already has, and gives packs a contract that is a pod template plus six environment variables.

Disable-and-mark is not out of the table. It changes how deletions are detected, not how hooks run, and it stays open in [#668](https://github.com/nebari-dev/nebari-infrastructure-core/issues/668). The hook contract and the marker are the same under either.

### How it works

**Keycloak.** NIC enables admin events on the `nebari` realm with details included and a seven day expiration. A user deletion is then readable at `GET /admin/realms/nebari/admin-events?resourceTypes=USER&operationTypes=DELETE`, with the user id in `resourcePath` and the username in `representation`.

**Packs.** A pack registers a `UserCleanupHook` (namespaced, `lifecycle.nebari.dev/v1alpha1`) per piece of per-user state. A hook has one stage and a pod template. `disable` runs as soon as the deletion is picked up and should be reversible. `delete` runs after the grace period. A pack that needs both registers two hooks. When a stage is due, the operator wraps the template in a Job in the hook's namespace, under the ServiceAccount in the template, and injects the user id, username, deletion time, stage, dry-run flag and marker name as environment variables. That is the whole contract.

Hooks are validated on registration: the rendered Job is submitted as a dry-run, the ServiceAccount must exist, and the namespace must carry `nebari.dev/managed=true`. The result is the `Accepted` condition. The same opt-in that gates a `NebariApp` gates hooks, so a namespace that was never opted in cannot receive the deletion stream.

**Operator.** A poller reads deletion events since a cursor kept in a ConfigMap and creates one `UserDeletion` (cluster-scoped) per event, named after the Keycloak user id. The `UserDeletion` reconciler records one entry per accepted hook, creates `disable` Jobs at once and `delete` Jobs once `deletedAt` plus the grace period has passed, and copies each Job's outcome onto its entry. The marker completes when the grace period has passed and every entry is terminal. It then stays as a tombstone for a retention period, and the reconciler deletes it afterwards.

Three properties follow from the names. A replayed event collides on the marker name, so a lost or reset cursor cannot run cleanup twice while the tombstone exists. Job names derive from user id, hook and stage, so a crash between creating a Job and writing status is recovered by adopting the Job on the next pass. The marker retention must be at least Keycloak's event retention, and the operator refuses to start otherwise.

**Configuration.** Six environment variables on the operator Deployment, set by NIC next to `KEYCLOAK_*`: poll interval (5m), grace period (30d), event retention (7d, must not exceed Keycloak's), marker retention (90d), and the cursor ConfigMap name and namespace.

### Consequences

**Good:**
- A pack author writes a pod template and reads six variables. Any image, any language, testable locally.
- Cleanup runs with the pack's identity in the pack's namespace. The operator never executes pack code as itself.
- Every deletion has a record: the marker shows which hooks ran, when, and how they ended. A hook left broken keeps its markers open rather than letting them complete silently.
- Nothing is installed inside Keycloak. The Keycloak side is one realm setting.

**Bad:**
- Only admin deletions are seen. A user deleting their own account through the account console produces a user event, not an admin event, and is invisible to the poller.
- An operator outage longer than the event retention loses the deletions Keycloak expired in between.
- Admin events switched off, on a realm NIC did not configure or after a manual change, means the poller sees nothing and reports nothing.
- The grace period is cluster-wide and frozen per marker. Packs cannot choose their own.
- A rejected hook is not re-evaluated when its ServiceAccount or namespace label appear later. Its spec has to change.
- The lifecycle controllers run in the operator's Deployment with the operator's Keycloak admin credentials. The intended split into a second Deployment with a read-only client is not done.

## Options Detail

### Option 1: Poll Keycloak admin events and run pack-registered Jobs

The chosen design, described above.

**Pros:**
- No component inside Keycloak, no new credentials, no new network path.
- Detection is idempotent by construction: the user id is the marker name.
- Packs register declaratively alongside the rest of their manifests.

**Cons:**
- Polling, so the `disable` stage lands within one poll interval, not immediately.
- Bounded by Keycloak's event retention on both the outage and the replay side.
- Self-service account deletion is not an admin event.

### Option 2: Disable-and-mark

An admin disables the user instead of deleting them. The platform records the disable, runs the `disable` hooks, and performs the final Keycloak delete itself when the grace period ends, after running the `delete` hooks.

**Pros:**
- The user record exists during the grace period, so a restore is a single flag flip and username reuse cannot happen in the window.
- The final delete is driven by the platform, so retention and deletion are one decision.

**Cons:**
- The platform has to delete users in Keycloak, which needs `manage-users` and conflicts with a read-only client.
- Whether Keycloak 26 refuses a brokered login for a disabled user who is linked to an upstream IdP, rather than sending them through first-broker-login, has to be confirmed.
- Admins have to learn that delete means disable. A direct delete bypasses the flow unless the poll from option 1 is kept as well.

### Option 3: Keycloak event listener SPI

A listener deployed into Keycloak pushes deletion events to the operator over HTTP.

**Pros:**
- Immediate, no polling.
- Sees user events as well as admin events, so self-service deletion is covered.

**Cons:**
- A Java artifact to build, version and deploy into Keycloak, which NIC does not do for anything else.
- The operator becomes a target Keycloak must reach, with delivery, retry and authentication to design.
- A missed push is lost unless the listener also persists, which reintroduces a cursor.

### Option 4: Each pack polls Keycloak

Every pack queries admin events itself and runs its own cleanup.

**Pros:**
- No new operator API.

**Cons:**
- Every pack needs Keycloak admin credentials and its own cursor, retry and replay handling.
- No shared record of what ran, no shared grace period, no single place to cancel.
- The same logic reimplemented per pack.

## Open Questions

- Cancel and retry as explicit actions on a marker. Deleting a marker is neither: the poller recreates it only if its event is still at the cursor.
- The split into a separate Deployment with a read-only Keycloak client. A sketch exists behind a `--controllers` flag on `feat/split-lifecycle-deployment`.
- Username reuse through an upstream identity provider. The `delete` stage should check whether the username is back in Keycloak and hold.
- Stage names. `disable` and `delete` are in the API; `immediate` and `afterGrace` were proposed.
- What the tombstone keeps. The username and the deleting admin's IP address are readable for the whole retention.
- A startup check that admin events are enabled on the realm, so a silent misconfiguration is at least logged.

## Links

- [nebari-operator#188](https://github.com/nebari-dev/nebari-operator/pull/188), the PoC
- [#668](https://github.com/nebari-dev/nebari-infrastructure-core/issues/668), design discussion
- [#673](https://github.com/nebari-dev/nebari-infrastructure-core/pull/673), admin events on the realm
- [ADR-0009](0009-declarative-keycloak-configuration.md), how NIC configures Keycloak
- `docs/lifecycle/` in nebari-operator, the operator and pack author documentation
