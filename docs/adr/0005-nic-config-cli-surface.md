# ADR-0005: nic config CLI surface

## Status

Proposed

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

**Deferred.** This ADR exists to enumerate options and surface the design questions. A decision should follow team discussion and/or feedback from the docs-site work.

The schema-pipeline PR series ships independently of this decision and does not foreclose any of the four options above.

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

1. **Required-from-omitempty signal.** Is `yaml:"<name>"` without `omitempty` an accurate-enough signal for "must be set on init"? Some required-ness is semantic (e.g. Hetzner's "exactly one node group must have `master: true`") and can't be expressed structurally. Acceptable to push those into `Validate()` and not surface them as flags?
2. **Optional-scalar flag coverage.** If we go Option 1 or 2, do flags cover only required scalars (clean rule, smaller `--help`) or some commonly-set optionals too (better ergonomics, larger flag matrix)?
3. **Composite-block-via-presence.** Is `--dns cloudflare --dns-zone-name example.com` → `dns:` block included a clear-enough rule, or do users want explicit `--with-dns cloudflare` toggles?
4. **`nic config schema` value-add.** With `schemas/` committed and fetchable, what's the user need for a runtime `nic config schema`? Air-gapped envs? Editor LSP integration? Just nice-to-have?
5. **Validation on init.** Run `client.Validate(...)` before emitting YAML, or trust the user to run `nic validate` after editing? The former catches errors earlier; the latter keeps init mechanical.

## Links

- [ADR-0004: Out-of-Tree Provider Plugin Architecture](0004-out-of-tree-provider-plugins.md) — related; if external providers can register, the schema and flag-gen mechanisms need to accommodate them.
