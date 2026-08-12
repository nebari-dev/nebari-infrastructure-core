# Config placeholders (`CHANGEME`)

NIC rejects any configuration that still contains unfilled placeholder values, so
running `nic deploy` on an unedited starter or example config fails fast with a
clear error instead of provisioning real infrastructure against nonsense values.

## The convention

The placeholder token is the literal string **`CHANGEME`** (case-sensitive). Any
string field whose value *contains* `CHANGEME` is treated as an unfilled
placeholder. This includes nested provider blocks, lists, and maps — the whole
config is scanned.

When a placeholder is found, validation fails before any provider API call with a
message naming the field path and the config file, for example:

```
configuration validation failed: placeholder value "CHANGEME" found in field
"cluster.aws.region"; edit the config before deploying (in config file "nebari-config.yaml")
```

The check runs at both `nic validate` and `nic deploy`.

## For example and starter authors

Use the exact token `CHANGEME` wherever the reader is expected to substitute
their own value, e.g.:

```yaml
project_name: CHANGEME
cluster:
  aws:
    region: CHANGEME
```

A literal sentinel is used deliberately, rather than pattern-matching values like
`example.com`: it is explicit, greppable, and never rejects a user who
legitimately owns such a value. Because the token is a substring match, avoid
using `CHANGEME` inside any value that is meant to be a valid default.

> Note: the configs under `examples/` intentionally use descriptive placeholders
> such as `my-nebari-*` and `nebari.example.com` (which are valid values), so
> they pass validation as-is. Use `CHANGEME` in starter configs that must be
> edited before they can deploy.
