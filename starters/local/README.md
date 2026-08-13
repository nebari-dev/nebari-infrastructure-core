# starter-local

A Nebi/Pixi starter workspace for a **local (kind)** Nebari deployment. kind
runs the whole cluster on your machine, so this starter needs **no cloud
credentials** and nothing to fill in but the project name.

## Import, edit, deploy

```bash
nebi import quay.io/nebari/starter-local:vX -o ./my-nebari
cd my-nebari
# edit the one placeholder:
$EDITOR config.yaml        # set project_name (it ships as CHANGEME)
nebi run validate          # rejects the unedited placeholder
nebi run deploy            # validate runs first, then brings up the kind cluster
```

`nebi import` refuses to write into a non-empty directory when the bundle
carries asset layers, so the import-then-edit cycle cannot clobber existing
work.

## The one field to edit

`config.yaml` ships with `project_name: CHANGEME`. That is the only field you
must change; `validate` fails until you do (`grep -n CHANGEME config.yaml`).
Everything else (self-signed certificate, `nebari.local` domain, Linux node
selectors) is preset for local development. `git_repository` is omitted on
purpose: nic auto-creates `~/.nic/gitops/<project_name>` and mounts it into the
kind cluster, so local dev is zero-config.

## nic on PATH

The tasks call `nic` directly. Once the prefix.dev github-releases channel is
live (#579), `pixi install` will resolve nic from `[dependencies]`. Until then,
**put a nic binary on your PATH** before running the tasks. nic does not need
kubectl (it talks to the cluster via embedded client-go); the optional `tools`
env (`pixi install -e tools`) adds `kubernetes-client` and `k9s` for
convenience on the platforms that carry a current kubectl.

## Secrets and where filled configs belong

Local kind needs no secrets. If you adapt this workspace, keep secrets in
environment variables, **never** in `config.yaml`. A filled config is a private
artifact: publish it only to a private registry or a nebi server, never to a
public one.
