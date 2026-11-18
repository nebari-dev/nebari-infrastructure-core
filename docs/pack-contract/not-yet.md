# Not yet

These were considered and are not in the contract. A pack that needs one solves it inside the pack for now. Any of them can be proposed through a platform RFD (see [Proposals and governance](policy/proposals.md)) once two packs need it.

- Shared ReadWriteMany storage. It is reconsidered when two packs need shared files and cannot design around them. Object storage is the default way to share data (proposed in ADR-0019, NIC #586).
- Automatic bucket creation (#660). It is the first candidate for the next contract version.
- cert-manager issuers for a pack's own internal certificates.
- TCP and TLS passthrough.
- Wildcard subdomains.
- GitOps write access.
- The Prometheus Operator.
- Extra cluster operators such as KubeRay.
- Backups for pack data.
