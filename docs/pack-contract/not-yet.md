# Not yet

These were considered and are not in the contract. A pack that needs one solves it inside the pack for now. Any of them can be proposed through a platform RFD (see [Proposals and governance](policy/proposals.md)) once two packs need it.

- Shared ReadWriteMany storage. It is reconsidered when two packs need shared files and cannot design around them. Object storage is the default way to share data (proposed in ADR-0019, NIC #586).
- Automatic bucket creation (#660). It is the first candidate for the next contract version.
- Pod-to-pod mTLS. It belongs to a service mesh, which issues short-lived workload certificates to a proxy so apps never handle them, not to each pack. A mesh can issue from the same root as the internal issuer (P18), for example through cert-manager's `istio-csr`.
- TCP and TLS passthrough.
- Wildcard subdomains.
- GitOps write access.
- The Prometheus Operator.
- Extra cluster operators such as KubeRay.
- Backups for pack data.
- A runtime platform-facts ConfigMap that pods read from the cluster. P5 delivers the facts as Helm values at render time, which covers what packs need today; a pod that needs a value at runtime gets it from an object the pack renders from `global.nebari`. The operator's OIDC client Secret also carries an `issuer-url` key today, but the operator specification does not name it, so it is not promised.
- A NebariApp `database` field (proposed in ADR-0007). It would be an experimental convenience on top of P4: the operator creates a CNPG Cluster with platform defaults and writes a credentials Secret with fixed keys (`host`, `port`, `username`, `password`, `database`, `uri`). Packs whose needs outgrow it keep using the CNPG API directly. It is also P4's path to an implementation-neutral interface, since any Postgres operator could satisfy it.
