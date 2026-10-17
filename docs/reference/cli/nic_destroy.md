## nic destroy

Destroy cloud infrastructure

### Synopsis

Destroys all infrastructure resources in reverse order of creation.
This includes node groups, EKS cluster, VPC, and IAM roles.

WARNING: This operation is destructive and cannot be undone. All data will be lost.

By default, you will be prompted to confirm before destruction begins.
Use --auto-approve to skip the confirmation prompt.

```
nic destroy [flags]
```

### Options

```
      --auto-approve      Skip confirmation prompt and destroy immediately
      --dry-run           Show what would be destroyed without actually deleting
  -f, --file string       Path to nebari-config.yaml file (auto-discovered if omitted)
      --force             Continue destruction even if some resources fail to delete
  -h, --help              help for destroy
      --log-file string   Write the run's full JSON log to this file (default ~/.nic/logs/<project>/<time>-<command>.jsonl)
      --no-log-file       Don't write a log file
      --timeout string    Override default timeout (e.g., '45m', '1h')
```

### Options inherited from parent commands

```
      --log-format string   Output format: auto, pretty, plain, json or github (auto is pretty on a terminal, github for deploy and destroy in GitHub Actions, json otherwise) (default "auto")
  -v, --verbose             Also show informational messages and OpenTofu output under each step
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

