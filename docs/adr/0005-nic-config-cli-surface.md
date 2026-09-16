# ADR-0005: nic config CLI surface

## Status

Proposed (2026-06-03) · Amended (2026-07-15) — config-reference generation pipeline resolved; see [Update](#update-2026-07-config-reference-pipeline-resolved-434) · Accepted (2026-09-01).

The discussion this ADR was opened for did not happen as a discussion. It was settled by [#552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552) shipping its outcomes instead: the starter workspaces ([#560](https://github.com/nebari-dev/nebari-infrastructure-core/issues/560)) and placeholder rejection ([#561](https://github.com/nebari-dev/nebari-infrastructure-core/issues/561)) now cover the onboarding need that Options 1 and 2 were reaching for. This PR records that outcome; the acceptance is the review approval on it.

## Date

2026-06-03

## Context

A separate PR series introduces a schema-generation pipeline in this repo that produces JSON Schema + YAML reference artifacts under `schemas/`, consumed by `nebari-docs`. That work intentionally stops short of any user-facing CLI for nebari-config bootstrapping or inspection: schemas are produced by an internal `cmd/schemagen` binary CI consumes, and the hand-written `examples/*.yaml` files stay as-is.

Several capabilities were explored during that design that we elected to defer for separate discussion rather than bundle in:

1. **`nic config init <provider>`** — emit a minimal-to-deploy starter YAML for a given provider, ready to fill in and `nic deploy`. Could replace / supersede the hand-written `examples/*.yaml` files.
2. **`nic config schema [<provider>] -o {json,yaml}`** — runtime equivalent of what `cmd/schemagen` produces at build time. Lets users inspect the schema without a network round-trip to GitHub or the docs site.
3. **Reflection-driven CLI flag generation** for `nic config init` from the Go `Config` types — required scalar fields become flags, godoc becomes `--help` text, composite blocks (DNS, certificate, gitops) are inferred from flag presence.
4. **Examples regeneration**: `examples/<provider>.yaml` maintained via `nic config init` invocations in CI, drift-gated the same way `schemas/` is.

This ADR is the venue for that discussion.

## Decision Drivers

- **Onboarding ergonomics.** A new user landing in this repo today needs to read provider examples, internalize the structure, and hand-craft a config. A scaffolding command could shortcut that.
- **Single source of truth.** Schema-gen establishes Go types as the source. The `nic config init` proposal extends that to bootstrap configs — `examples/` becomes a generated artifact rather than hand-written.
- **CLI surface area.** `nic` is currently lean (`deploy`, `destroy`, `validate`, `kubeconfig`, `version`). Adding `config init` + `config schema` is two subcommands; the flag matrix on `init` is potentially much larger.
- **Maintenance cost of generated examples.** Hand-written `examples/*.yaml` drift silently when fields change; CI-gated regen catches drift only if examples are generated.
- **Reflection complexity.** Reflection-driven flag generation works cleanly for scalars, struggles with maps/slices/nested structs, and adds non-trivial code that needs its own test coverage.

## Considered Options

1. **`nic config init` only.** Scaffolding command with reflection-driven flags. No `nic config schema` (users get JSON Schema via the committed `schemas/`). Regenerate `examples/*.yaml` from `init` in CI.
2. **Full `nic config` surface.** Both `nic config init` and `nic config schema [-o json|yaml]`. `cmd/schemagen` may stay as the CI mechanism or be retired in favor of `nic config schema`.
3. **Status quo.** No `nic config` subcommands. `cmd/schemagen` stays. `examples/*.yaml` stays hand-written; drifts silently.
4. **Replace `examples/` with `schemas/`.** Drop `examples/` entirely; users learn the config by reading `schemas/<provider>.yaml`. No scaffolding command.

## Decision Outcome

Chosen option: **Option 3, status quo**, because the two capabilities Options 1
and 2 were reaching for are both delivered outside the CLI, by mechanisms that turned out to
be better than a subcommand.

- **Config bootstrap** is a generated Nebi starter workspace
  ([#560](https://github.com/nebari-dev/nebari-infrastructure-core/issues/560)).
  `nebi import quay.io/nebari/starters/aws:<tag>` yields a ready-to-edit `config.yaml`, and
  additionally pins the `nic` binary in `pixi.lock` and ships the `validate` and `deploy`
  tasks. `nic config init` would have produced only the first of those three.
- **Schema inspection** is served today by the generated config reference under
  `docs/configuration/` (docgen, #434). A machine-readable JSON Schema consumed through an
  editor `$schema` modeline is in flight
  ([#562](https://github.com/nebari-dev/nebari-infrastructure-core/issues/562),
  [#600](https://github.com/nebari-dev/nebari-infrastructure-core/issues/600)) and, per the
  2026-07 update below, arrives as a docgen output format rather than as a
  `nic config schema` subcommand.

The argument for the starter over a generator rests on two things the Options Detail below
does not already cover:

- **It versions.** Starters are tagged OCI artifacts, so `nebi diff` shows what config
  changed between two `nic` versions, and rollback is pulling the older tag. A generator
  emits a file and forgets.
- **It pins the toolchain with the config.** The workspace's `pixi.lock` fixes the exact
  `nic` build, `nic` pins OpenTofu, and the embedded `.terraform.lock.hcl` pins the
  providers, so one lockfile transitively pins the whole infrastructure toolchain.

Options 1 and 4 are rejected on the Cons already recorded below, with one thing that has
changed since they were written: `examples/` is now the source `cmd/starters` renders the
starter workspaces from, and every file in it must validate as-is against the Go config
types in CI (`pkg/nic.TestExampleConfigsValidate`), which also rejects unreplaced
`CHANGEME` placeholders. The silent drift Option 4 existed to solve is caught by that test
rather than by deleting the directory. Note its limit, per its own docstring: provider-level
validation is not reached, so a green result does not prove an example would deploy.

### Consequences

**Good:**

- Onboarding gets a deployable starting point that pins its own toolchain, which neither
  `examples/` nor a generator provided.
- A Nebari upgrade becomes a reviewable config diff, and rollback becomes pulling a tag.
- No reflection code to write or maintain: none of the maps, slices, pointers or tri-state
  `*bool` handling Option 1's Cons priced.

**Bad:**

- No machine-readable schema until #562 and #600 land, so editor completion and offline
  schema inspection are unavailable in the meantime.
- Onboarding now depends on Nebi, pixi and an OCI registry (`quay.io/nebari`) being
  reachable. That is a materially heavier dependency chain than a `nic` subcommand, and it
  moves part of the onboarding path outside this repo.
- `examples/` stays hand-maintained. CI catches drift only against the Go types, which does
  not prove an example deploys.

### Distribution belongs in its own ADR

[#552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552) also settled how
`nic` reaches users: a prefix.dev conda channel repackaging the release archives, the package
name `nebari-infrastructure-core`
([#556](https://github.com/nebari-dev/nebari-infrastructure-core/issues/556)), and starter
workspaces published as OCI artifacts. Those are recorded here only where they bear on config
bootstrap. The packaging concerns proper, including whether to move to the shared
`github-releases` channel
([#620](https://github.com/nebari-dev/nebari-infrastructure-core/issues/620)), have different
drivers and would be unfindable under a title about the config CLI surface.

**Decision: distribution gets its own ADR, written separately.** Tracked in
[#652](https://github.com/nebari-dev/nebari-infrastructure-core/issues/652).

## Options Detail

### Option 1: `nic config init` only

User-facing surface:

```bash
nic config init aws \
  --project-name my-cluster \
  --region us-west-2 \
  --kubernetes-version 1.34 \
  --domain example.com \
  --certificate-type letsencrypt --certificate-email admin@example.com \
  --dns cloudflare --dns-zone-name example.com \
  > my-config.yaml
```

Reflection over each provider's `Config` type produces flags. Signal for required-ness: `yaml:"..."` without `omitempty`. Composite blocks (DNS, certificate, gitops) inferred from flag presence — if any `--dns-*` flag is set, the `dns:` block lands in the output; otherwise omitted.

Missing required flags fail fast with Cobra's native message. After flag binding, the populated config passes through `client.Validate(...)` before YAML is written.

Map / slice / nested-struct fields don't become flags — the init output carries a sensible default the user edits in the YAML (`node_groups`, `tags`, `availability_zones`).

**Pros:**
- Onboarding: zero-to-deployable in one command for the happy paths.
- `examples/` stay in sync with code via CI regen + drift gate.
- Same metadata drives schema-gen and `--help`.

**Cons:**
- Non-trivial reflection code with edge cases (slices, pointers, tri-state `*bool`).
- Flag matrix per provider can get unwieldy if it grows to optionals.
- Doesn't address schema inspection without network access.

### Option 2: Full `nic config` surface

Adds `nic config schema [-o json|yaml]` on top of Option 1. Same metadata source.

If retained, `cmd/schemagen` becomes a thin wrapper around `nic config schema` for CI, or is retired.

**Pros:**
- Symmetric: users can both produce starters and inspect the full reference offline.
- Single mechanism (the `nic` binary) for producer and consumer.

**Cons:**
- Adds Option 1's reflection complexity plus `nic config schema` itself.
- `nic config schema` is mostly useful when `schemas/` isn't already published — diminishing return for users who have GitHub access.

### Option 3: Status quo (Chosen)

No `nic config` subcommands. `cmd/schemagen` ships from the schema-pipeline PRs. `examples/*.yaml` stays hand-written, drifts silently when fields change.

**Pros:**
- Smallest CLI surface.
- Zero reflection code.

**Cons:**
- Onboarding stays manual.
- Examples drift silently.

### Option 4: Replace `examples/` with `schemas/`

Drop `examples/` entirely. The committed `schemas/<provider>.yaml` (a fully-commented values-yaml-like document) serves as both reference *and* starter — users copy it, uncomment what they need, fill in required fields.

**Pros:**
- Single artifact, single source of truth.
- No drift, no scaffolding command, no reflection.

**Cons:**
- A 300+ line "minimum config" YAML is a worse onboarding experience than a 20-line deployable starter.
- Removes the curated happy-path content that exists today.

## Open questions for discussion

Resolved 2026-09-01 by the decision above. The questions are kept verbatim; each answer follows it.

1. **Required-from-omitempty signal.** Is `yaml:"<name>"` without `omitempty` an accurate-enough signal for "must be set on init"? Some required-ness is semantic (e.g. Hetzner's "exactly one node group must have `master: true`") and can't be expressed structurally. Acceptable to push those into `Validate()` and not surface them as flags?

   *Moot for flag generation, load-bearing elsewhere.* No flags are generated, so this no longer gates the ADR. The signal itself did not go away: docgen relies on it, and the JSON Schema emitter in flight makes it stricter, because a schema marking a runtime-defaulted field as `required` rejects configs the binary accepts. The semantic cases named here stay in `Validate()`, which is where the question always pointed. See also the 2026-07 update's data point below.

2. **Optional-scalar flag coverage.** If we go Option 1 or 2, do flags cover only required scalars (clean rule, smaller `--help`) or some commonly-set optionals too (better ergonomics, larger flag matrix)?

   *Moot.* No flags are generated.

3. **Composite-block-via-presence.** Is `--dns cloudflare --dns-zone-name example.com` → `dns:` block included a clear-enough rule, or do users want explicit `--with-dns cloudflare` toggles?

   *Moot.* No flags are generated.

4. **`nic config schema` value-add.** With `schemas/` committed and fetchable, what's the user need for a runtime `nic config schema`? Air-gapped envs? Editor LSP integration? Just nice-to-have?

   *Answered: no need that justifies a subcommand.* Of the three motivations listed, editor/LSP integration is the real one, and a published schema pinned to the same version as the binary serves it better than a runtime command an editor cannot invoke per keystroke. Air-gapped inspection is served by the generated reference travelling in the source tree.

5. **Validation on init.** Run `client.Validate(...)` before emitting YAML, or trust the user to run `nic validate` after editing? The former catches errors earlier; the latter keeps init mechanical.

   *Answered by the starter's task graph.* There is no `init` to validate at. The starter's `deploy` task depends on `validate`, so validation runs before any deploy rather than at creation, and #561 makes `validate` reject unreplaced placeholder values - the specific failure mode this question worried about, caught when it matters instead of when the file is created.

## Update (2026-07): config-reference pipeline resolved (#434)

PR #434 shipped `cmd/docgen`, which auto-generates both reference surfaces from
the source of truth in this repo, drift-gated in CI (satisfying #440 and its
upstream driver nebari-docs#665):

- **CLI reference** - one markdown page per command in `docs/reference/cli/`,
  produced by walking the real `internal/cli.NewRootCmd()` tree with `cobra/doc`.
- **Config schema reference** - per-provider and core markdown pages in
  `docs/configuration/`, produced by parsing the Go config structs with `go/ast`
  (so field doc comments become descriptions, which reflection cannot see) and
  discovering provider config files by glob.

This resolves the config-reference pipeline that the Context above assumed would
be a separate `cmd/schemagen` binary emitting JSON Schema + YAML under `schemas/`:

- **docgen is the authoritative config-reference generator.** The
  `cmd/schemagen` / `schemas/` pipeline described in the Context did not ship and
  is superseded for the reference-docs concern. NIC emits finished markdown and
  `nebari-docs` consumes it at build time (per nebari-docs#665), rather than NIC
  emitting JSON Schema for `nebari-docs` to render into markdown downstream.
- **If JSON Schema output is ever needed, it is a docgen output format, not a
  second binary.** Editor/LSP validation of `config.yaml`, offline schema
  inspection, or the `nic config schema` subcommand in Option 2 would each be
  served by adding a format flag to docgen, reusing its existing `go/ast` parse
  and provider discovery. A separate generator reading the same Go structs would
  create exactly the two-sources-of-truth drift this generation effort exists to
  prevent.

**Scope of this update.** This resolves only the *generation pipeline* (how
config and CLI reference material is produced and where it lives). It does **not**
decide the user-facing `nic config` surface that is the main subject of this ADR:
`nic config init` and `nic config schema` (Options 1 and 2) remain **Deferred /
Proposed**. What changes is their substrate: those options would extend
`cmd/docgen`, and Option 2's "`cmd/schemagen` may stay as the CI mechanism or be
retired" is now moot, since the CI mechanism is docgen.

**Data point for open question #1 (required-from-omitempty).** docgen relies on
exactly that signal (`yaml:"name"` without `omitempty` implies required). The
#434 review confirmed it is imperfect: `kubernetes_version` was tagged without
`omitempty` yet is optional at validation time. docgen's mitigation is to treat
pointer fields as optional and to add `omitempty` to genuinely-optional tags, but
structurally-required-yet-semantically-optional cases still have to live in
`Validate()`. Any `nic config init` flag-generation would inherit this same
limitation.

## Links

- [ADR-0004: Out-of-Tree Provider Plugin Architecture](0004-out-of-tree-provider-plugins.md)
- [#552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552) Epic: distribute NIC via pixi/prefix.dev and ship Nebari deployments as Nebi workspaces - the epic this decision serves
- [#565](https://github.com/nebari-dev/nebari-infrastructure-core/issues/565) The issue asking for this decision to be recorded
- [#560](https://github.com/nebari-dev/nebari-infrastructure-core/issues/560) Publish per-provider Nebi starter workspaces - the config-bootstrap mechanism chosen here
- [#561](https://github.com/nebari-dev/nebari-infrastructure-core/issues/561) `nic validate` must reject placeholder config values - answers open question 5
- [#562](https://github.com/nebari-dev/nebari-infrastructure-core/issues/562) Version-pinned `$schema` modeline in starter configs - answers open question 4, in flight
- [#579](https://github.com/nebari-dev/nebari-infrastructure-core/issues/579) Distribute nic via a prefix.dev channel - the distribution decision deferred to its own ADR
- [#556](https://github.com/nebari-dev/nebari-infrastructure-core/issues/556) Conda package name for nic
- [#620](https://github.com/nebari-dev/nebari-infrastructure-core/issues/620) Move to the shared prefix.dev github-releases channel - open
- [#652](https://github.com/nebari-dev/nebari-infrastructure-core/issues/652) Write the distribution and packaging ADR — related; if external providers can register, the schema and flag-gen mechanisms need to accommodate them.
