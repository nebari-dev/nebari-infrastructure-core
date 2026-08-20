# Generated configuration schemas

**Do not edit these files by hand.** Everything in this directory except this
README is generated from the Go config types by `cmd/docgen`, the same tool that
generates `docs/configuration/` and `docs/reference/cli/`.

- `manifest.json` — index the docs site fetches first (`providers` — the cluster
  providers, under the name the docs site already consumes — plus `dns`,
  `repository`, and `top_level`); carries a `_comment` marker since JSON has no
  comment syntax.
- `nebari-config.json` — the top-level `nebari-config.yaml` schema.
- `providers/<name>.json` — one JSON Schema per registered provider. Cluster and
  DNS providers keep bare filenames; other categories are qualified with their
  group (`repository-local.json`), since `cluster/local` and `repository/local`
  share a name.

The provider list comes from the NIC registry, not a hand-maintained list:
registering a provider that implements the optional `ConfigTyped` capability
extends this directory automatically.

## Regenerating

```sh
make docs   # regenerates docs/ and schemas/ together
```

Run this after changing any config struct, its `yaml`/`jsonschema` tags, or its
godoc, then commit the result. CI regenerates and fails on drift, so an
out-of-date tree is caught in review.

Note that `jsonschema:"enum=…"` and `jsonschema:"default=…"` are read by both
emitters — declare a field's allowed values or default in the tag and they
appear here *and* in the markdown reference. Don't restate them in the godoc.

## Consumers

The `nebari-docs` site fetches `manifest.json` and the referenced files at
runtime to render the configuration reference. Tagging a release gives that
reference a versioned schema for free.

These documents also validate a real `nebari-config.yaml`, which is what makes
them useful to an editor or LSP. `pkg/nic.TestExampleConfigsMatchGeneratedSchemas`
holds that property: every file under `examples/` is validated against
`nebari-config.json` and against the schema for each provider block it uses, so
a schema that drifts from what NIC accepts fails in CI rather than in someone's
editor.

Two things to know about the strictness:

- **Objects are closed** (`additionalProperties: false`). That is stricter than
  the runtime decoder, which ignores keys it does not recognise. Catching a
  typo'd key is the point: at runtime such a key parses and then silently does
  nothing.
- **Required-ness is inferred** from a `yaml` tag without `,omitempty`. A field
  NIC defaults at runtime must carry `,omitempty`, or the schema will reject
  configs the binary accepts.
