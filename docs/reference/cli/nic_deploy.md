## nic deploy

Deploy infrastructure based on configuration file

### Synopsis

Deploy cloud infrastructure and Kubernetes resources based on the
provided nebari-config.yaml file. This command will create all necessary
resources to establish a fully functional Nebari cluster.

Configs that still contain the reserved CHANGEME placeholder are rejected before
any provider API call, so an unedited starter cannot provision infrastructure.

Use --dry-run to preview changes without applying them.

Once the cluster is up, the NIC build that deployed it is recorded in the
kube-system/nic-deployment-info ConfigMap, so the version is queryable from
inside the cluster.

```
nic deploy [flags]
```

### Options

```
      --dry-run          Show what would be deployed without making changes
  -f, --file string      Path to nebari-config.yaml file (auto-discovered if omitted)
  -h, --help             help for deploy
      --regen-apps       Regenerate ArgoCD application manifests even if already bootstrapped
      --timeout string   Override default timeout (e.g., '45m', '1h')
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

