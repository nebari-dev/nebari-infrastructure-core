# Provided but not promised

These exist on some targets and you may use them, but your pack must work, or fail with a clear message, where they are missing.

- Longhorn and its ReadWriteMany volumes. Hetzner has it by default. AWS and existing clusters have it by opt-in.
- The EFS class `efs-sc` on AWS.
- The GPU operator on AWS.
- The ADR-0014 values overlay seam. It is for human-authored overlays only.
- The ADR-0008 collector override. It has a single owner.
