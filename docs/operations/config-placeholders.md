# Config placeholders (`CHANGEME`)

NIC rejects any configuration that still contains unfilled placeholder values, so
running `nic deploy` on an unedited starter or example config fails fast with a
clear error instead of provisioning real infrastructure against nonsense values.

## The convention

The placeholder token is the literal string **`CHANGEME`** (case-sensitive). The
check walks the parsed YAML node tree, so any scalar value *or mapping key* whose
text *contains* `CHANGEME` is treated as an unfilled placeholder. This includes
nested provider blocks, lists, and map keys (e.g. `node_groups: { CHANGEME: … }`)
— the whole config is scanned in one pass, and every offending field is reported
together rather than just the first.

Multi-line values written as block scalars (`|` or `>`) are scanned too, so a
placeholder inside a stubbed certificate or SSH key is caught:

```yaml
certificate:
  existing:
    fullchain: |
      -----BEGIN CERTIFICATE-----
      CHANGEME
      -----END CERTIFICATE-----
```

Placeholders in **comments are ignored**: only scalar values and mapping keys are
scanned, so a `# CHANGEME` reminder in a comment does not trip the check.

When a placeholder is found, validation fails before any provider API call with a
message naming the field path(s) and the config file, for example:

```
placeholder value "CHANGEME" found in fields "cluster.aws.node_groups.CHANGEME",
"project_name"; edit the config before deploying (in config file "nebari-config.yaml")
```

The check runs at `nic validate` and `nic deploy` only. It does **not** gate
`nic destroy` or `nic kubeconfig`, which only need a parseable config and must keep
working against a cluster that was deployed from an already-edited file.

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
