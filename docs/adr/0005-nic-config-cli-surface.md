# ADR-0005: nic config CLI surface

## Status

Accepted (2026-09-01). Proposed (2026-06-03) - Amended (2026-07-15): config-reference generation pipeline resolved; see [Update](#update-2026-07-config-reference-pipeline-resolved-434).

## Date

2026-06-03

## Context

A separate PR series introduces a schema-generation pipeline in this repo that produces JSON Schema + YAML reference artifacts under `schemas/`, consumed by `nebari-docs`. That work intentionally stops short of any user-facing CLI for nebari-config bootstrapping or inspection: schemas are produced by an internal `cmd/schemagen` binary CI consumes, and the hand-written `examples/*.yaml` files stay as-is.

Several capabilities were explored during that design that we elected to defer for separate discussion rather than bundle in:

1. **`nic config init <provider>`** — emit a minimal-to-deploy starter YAML for a given provider, ready to fill in and `nic deploy`. Could replace / supersede the hand-written `examples/*.yaml` files.
2. **`nic config schema [<provider>] -o {json,yaml}`** — runtime equivalent of what `cmd/schemagen` produces at build time. Lets users inspect the schema without a network round-trip to GitHub or the docs site.
3. **Reflection-driven CLI flag generation** for `nic config init` from the Go `Config` types — required scalar fields become flags, godoc becomes `--help` text, composite blocks (DNS, certificate, gitops) are inferred from flag presence.
4. **Examples regeneration**: `examples/<provider>.yaml` maintained via `nic config init` invocations in CI, drift-gated the same way `schemas/` is.

This ADR is the venue for that discussion. It stays in Proposed status until the team converges.

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

**Accepted: Option 3 (status quo CLI surface), with the onboarding need met outside the CLI.**

No `nic config init`, no `nic config schema`. The two capabilities those options were reaching for are both delivered, by mechanisms that turned out to be better than a subcommand:

- **Config bootstrap** is a generated Nebi starter workspace (#560). `nebi import quay.io/nebari/starters/aws:v0.14.0` yields a ready-to-edit `config.yaml`, and additionally pins the `nic` binary in `pixi.lock` and ships the `validate` and `deploy` tasks. `nic config init` would have produced only the first of those three.
- **Schema inspection** is the JSON Schema committed under `schemas/` and consumed by an editor through a `$schema` modeline (#562). This serves the need continuously, on every edit, rather than once at creation.

### Why the starter beats the generator

- **It versions.** Starters are tagged OCI artifacts, so `nebi diff` shows what config changed between two `nic` versions, and rollback is pulling the older tag. A generator has no such notion: it emits a file and forgets.
- **It pins the toolchain with the config.** The workspace's `pixi.lock` fixes the exact `nic` build, `nic` pins OpenTofu, and the embedded `.terraform.lock.hcl` pins the providers. One lockfile transitively pins the whole infrastructure toolchain. This is the property the epic (#552) was built to get, and a scaffolding command cannot offer it.
- **It avoids the reflection cost this ADR itself priced.** Option 1's Cons list maps, slices, pointers and tri-state `*bool`, plus semantically-required-but-structurally-invisible rules such as Hetzner's "exactly one node group must have `master: true`". None of that has to be written.
- **The surface was never the bottleneck.** Stripped of comments, the examples are 51 effective lines for AWS, 39 for GCP, 16 for Hetzner and 15 for existing-cluster. Copy-and-edit at that size is not what makes onboarding hard; not knowing which keys are valid is, and the schema answers that.

### What this does not decide

Nothing here forecloses a `nic config init` later. If starters prove insufficient, Option 1 remains available and its substrate is now `cmd/docgen` rather than a new generator (see the two updates below). The decision recorded here is that it is not needed, not that it is impossible.

### Examples stay hand-written

Option 4 (replace `examples/` with `schemas/`) is rejected for the reason already in its Cons: a 300-line fully-commented document is a worse starting point than a 20-line deployable one. `examples/` also gained a job it did not have when this ADR was written - it is the source `cmd/starters` renders the starter workspaces from, and every file in it is validated against the generated schemas in CI (`pkg/nic.TestExampleConfigsMatchGeneratedSchemas`). The drift that Option 4 existed to solve is now caught by that test rather than by deleting the directory.

### Distribution belongs in its own ADR

This ADR's subject is the config surface, and #552 also settled how `nic` reaches
users: a prefix.dev conda channel repackaging the release archives, the package
name `nebari-infrastructure-core` (#556), and starter workspaces published as OCI
artifacts to `quay.io/nebari` (#560).

Those decisions are recorded here only where they bear on config bootstrap - the
starter workspace as the mechanism, above. The packaging concerns proper (which
channel, how release archives become conda packages, what the trusted-publisher
and signing chain is, and whether to move to the shared `github-releases` channel
per #620) are a different subject with a different set of drivers, and folding
them into an ADR titled "nic config CLI surface" would leave them unfindable.

**Decision: distribution gets ADR-0017, written separately.** Tracked in #652.

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

### Option 3: Status quo

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

Resolved 2026-09-01 with the decision above. Recorded rather than deleted, since the reasoning is what a future reader needs.

1. **Required-from-omitempty signal.** *Moot for flag generation, load-bearing elsewhere.* No flags are generated, so the question no longer gates this ADR. The signal did not go away, though: both docgen emitters rely on it, and the JSON Schema emitter made it stricter, because a schema marking a runtime-defaulted field as `required` rejects configs the binary accepts. Semantic requirements such as Hetzner's `master: true` rule stay in `Validate()`, which is where the question always pointed.
2. **Optional-scalar flag coverage.** *Moot.* No flags.
3. **Composite-block-via-presence.** *Moot.* No flags.
4. **`nic config schema` value-add.** *Answered: none that justifies a subcommand.* Of the three motivations listed, editor/LSP integration is the real one, and it is served better by a published, version-pinned schema than by a runtime command - an editor cannot invoke a CLI per keystroke, and the modeline pins the schema to the same version as the binary. Air-gapped inspection is served by the `schemas/` directory travelling in the source tree and in the starter workspace.
5. **Validation on init.** *Answered by the starter's task graph.* There is no `init` to validate at. The starter's `deploy` task depends on `validate` (#560), so validation runs before any deploy rather than at creation, and #561 makes `validate` reject unreplaced placeholder values - which is the specific failure mode "validate on init" was worried about, caught at the moment it matters instead of the moment the file is created.

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

- [ADR-0004: Out-of-Tree Provider Plugin Architecture](0004-out-of-tree-provider-plugins.md) — related; if external providers can register, the schema and flag-gen mechanisms need to accommodate them.
