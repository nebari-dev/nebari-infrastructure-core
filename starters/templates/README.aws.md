# Nebari on AWS - starter workspace

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

`project_name`, `domain`, the ACME email and the GitOps repository URL. The
infrastructure defaults (region, availability zones, instance types, Longhorn,
EFS) are working values, not placeholders. **If you change `region`, change
`availability_zones` to match**: `nic validate` does not cross-check them and a
mismatch fails mid-deploy.

`nic validate` runs offline and needs no AWS credentials. It will not catch a
bad region, a nonexistent instance type or an availability zone that does not
exist in your region; those surface at deploy time.

## Cost note

This starter inherits the production-recommended shape, including dedicated
Longhorn storage nodes with large gp3 volumes and EFS. That is a real monthly
bill; trim the node groups for experiments.

## Pinning

`pixi.lock` pins the exact toolchain. `nic` pins OpenTofu, and the embedded
`.terraform.lock.hcl` pins provider versions, so one lockfile transitively pins
the whole stack. Commit `pixi.lock`.

This file is copied verbatim into the published starter. Edit it here.
