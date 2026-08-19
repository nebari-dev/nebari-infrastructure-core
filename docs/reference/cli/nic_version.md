## nic version

Show version information

### Synopsis

Display the version information for Nebari Infrastructure Core (NIC).

This reports the identity of the binary in your hand. To ask which NIC build
produced a cluster that is already running, read the ConfigMap nic deploy
writes into it:

  kubectl get cm nic-deployment-info -n kube-system \
    -o jsonpath='{.data.nic-version}@{.data.nic-commit}'

```
nic version [flags]
```

### Options

```
  -h, --help   help for version
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

