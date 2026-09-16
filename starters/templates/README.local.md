# Nebari on local (kind) - starter workspace

A pinned Pixi/Nebi workspace for deploying Nebari Infrastructure Core (NIC).
It ships the toolchain, a placeholder `config.yaml`, and the deploy tasks, so a
whole deployment travels together as one versioned, lock-pinned unit.

## Quick start

```bash
# Fill in the placeholders (everything set to CHANGEME):
grep -n CHANGEME config.yaml
$EDITOR config.yaml

# Install the pinned toolchain:
pixi install

# Validate, then deploy:
pixi run validate
pixi run deploy      # runs validate first (task depends-on)
```

`nic validate` rejects any value still containing `CHANGEME`, so an unedited
workspace fails fast instead of attempting a real deploy.

## What you must edit

Only `project_name`. The local provider runs everything in a kind cluster on
your machine, so it needs no cloud credentials, the certificate is self-signed
and the GitOps repository is created for you.

## Pinning

`pixi.lock` pins the exact toolchain. Commit it. The local provider drives kind
through an embedded Go library, so there is no OpenTofu in this workspace and
nothing else to pin.

This file is copied verbatim into the published starter. Edit it here.
