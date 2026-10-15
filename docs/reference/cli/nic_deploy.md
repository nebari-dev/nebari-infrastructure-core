## nic deploy

Deploy infrastructure based on configuration file

### Synopsis

Deploy cloud infrastructure and Kubernetes resources based on the
provided nebari-config.yaml file. This command will create all necessary
resources to establish a fully functional Nebari cluster.

Configs that still contain the reserved CHANGEME placeholder are rejected before
any provider API call, so an unedited starter cannot provision infrastructure.

Use --dry-run to preview changes without applying them.

```
nic deploy [flags]
```

### Options

```
      --dry-run           Show what would be deployed without making changes
  -f, --file string       Path to nebari-config.yaml file (auto-discovered if omitted)
  -h, --help              help for deploy
      --log-file string   Write the run's full JSON log to this file (default ~/.nic/logs/<project>/<time>-<command>.jsonl)
      --no-log-file       Don't write a log file
      --regen-apps        Regenerate ArgoCD application manifests even if already bootstrapped
      --timeout string    Override default timeout (e.g., '45m', '1h')
```

### Options inherited from parent commands

```
      --log-format string   Output format: auto, pretty, plain, json or github (auto is pretty on a terminal, json otherwise) (default "auto")
  -v, --verbose             Also show informational messages and OpenTofu output under each step
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

