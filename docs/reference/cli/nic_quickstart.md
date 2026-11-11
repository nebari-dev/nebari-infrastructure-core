## nic quickstart

Deploy Nebari on a local kind cluster with Nebi and the Data Science Pack

### Synopsis

Deploy Nebari on a local kind cluster, with Nebi and the Data Science Pack
installed on top.

quickstart writes a regular NIC config file (config.yaml by default) and
deploys it. Afterwards, edit that file and run 'nic deploy' to change the
cluster, or run 'nic destroy' to delete it.

The software packs are written into the local GitOps repository, where ArgoCD
installs them. Edit or delete those files to change or remove the packs.

Use --dry-run to write the config file without deploying.

```
nic quickstart [flags]
```

### Options

```
      --domain string    Domain the platform is served on (default "nebari.lvh.me")
      --dry-run          Write the config file without deploying
      --force            Overwrite the config file if it already exists with different content
  -h, --help             help for quickstart
      --http-port int    Host port for HTTP (default 80)
      --https-port int   Host port for HTTPS (default 443)
  -o, --out string       Path to write the generated config file to (default "config.yaml")
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

