## nic outputs

Print the deployed platform's entry points

### Synopsis

Print the entry points of a deployed Nebari platform: its domain, the
Keycloak issuer URL, the gateway address, and the bootstrap admin passwords for
Keycloak and Argo CD.

Because the same binary renders the manifests that place these objects, the
reported names and formulas always match the platform it deployed. Consumers
should call this command rather than reading secrets out of the cluster
themselves, which goes stale silently when a release moves an object.

Every field either resolves or the command exits non-zero naming each field it
could not read. Some fields materialize after a deploy returns (the Argo CD
server writes its own initial admin secret; the gateway address waits on the
load balancer) - use --wait to poll for those.

Secret values are redacted unless --show-secrets is passed. Under --format json
a redacted field is null and is listed under a "redacted" key, so a caller that
forgets the flag gets null rather than a placeholder it might use as a password.
Status messages go to stderr, so --format json is safe to pipe.

```
nic outputs [flags]
```

### Examples

```
  # Human-readable summary, secrets redacted
  nic outputs

  # Machine-readable, for scripts and CI
  nic outputs --format json --show-secrets

  # Immediately after a deploy, while the platform is still converging
  nic outputs --format json --show-secrets --wait --timeout 10m
```

### Options

```
  -f, --file string        Path to nebari-config.yaml file (auto-discovered if omitted)
      --format string      Output format: table or json (default "table")
  -h, --help               help for outputs
      --show-secrets       Print secret values instead of redacting them
      --timeout duration   How long to poll when --wait is set (default 5m0s)
      --wait               Poll for outputs that are not yet available
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

