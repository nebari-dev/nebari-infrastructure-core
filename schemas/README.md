# Generated configuration schemas

**Do not edit these files by hand.** Everything in this directory except this
README is generated from the Go config types by `cmd/schemagen`.

- `manifest.json` — index the docs site fetches first (`providers`, `dns`,
  `top_level`); carries a `_comment` marker since JSON has no comment syntax.
- `nebari-config.json` — the top-level `nebari-config.yaml` schema.
- `providers/<name>.json` — one JSON Schema per registered cluster/DNS provider.

## Regenerating

```sh
make schemas   # go run ./cmd/schemagen -out ./schemas
```

Run this after changing any config struct, its `yaml`/`jsonschema` tags, or its
godoc, then commit the result. The `Schemas` CI workflow regenerates and fails
on drift, so an out-of-date tree is caught in review.

## Consumers

The `nebari-docs` site fetches `manifest.json` and the referenced files at
runtime to render the configuration reference. Tagging a release gives that
reference a versioned schema for free.
