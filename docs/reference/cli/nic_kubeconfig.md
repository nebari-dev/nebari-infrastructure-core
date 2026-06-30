## nic kubeconfig

Generate kubeconfig for the deployed Nebari cluster

### Synopsis

Generate and output the kubeconfig file for accessing the Kubernetes
cluster deployed by Nebari. This command retrieves the necessary cluster
information and constructs a kubeconfig file that can be used with kubectl
or other Kubernetes clients.

```
nic kubeconfig [flags]
```

### Options

```
  -f, --file string     Path to nebari-config.yaml file (auto-discovered if omitted)
  -h, --help            help for kubeconfig
  -o, --output string   Path to output kubeconfig file (defaults to stdout)
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

