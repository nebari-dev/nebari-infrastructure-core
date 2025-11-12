# Open Questions

### 13.1 Technical Questions

1. **State Encryption:** Should state files be encrypted at rest? (Recommendation: Yes, use cloud-native encryption)
2. **Multi-Cluster:** How to manage multiple clusters in one state file? (Options: separate states, or cluster array in state)
3. **Custom Kubernetes Distributions:** Support for k0s, k3d, RKE2? (v1: No, v2: Maybe)
4. **Helm Chart Storage:** Where to store foundational software Helm charts? (OCI registry? Git?)
5. **Operator HA:** Should operator run in HA mode (multiple replicas)? (Recommendation: Yes, with leader election)

### 13.2 Configuration Questions

6. **Config Validation:** Schema validation via JSON Schema or custom Go validation? (Recommendation: Custom Go + JSON Schema for IDE support)
7. **Config Inheritance:** Support for base + overlay configs? (Recommendation: Yes, via `extends` field)
8. **Secrets Management:** How to handle secrets in config (Keycloak admin password, etc.)? (Options: external secrets operator, sealed secrets, cloud secrets manager)

### 13.3 Deployment Questions

9. **Rollback Strategy:** Should `nic rollback` be a command? (Recommendation: Yes, v1.1)
10. **Blue/Green Deployments:** Support for blue/green cluster deployments? (Recommendation: v2.0+)
11. **Canary Deployments:** For foundational software updates? (Recommendation: v2.0+)

### 13.4 Integration Questions

12. **CI/CD Integration:** Should NIC provide GitHub Actions / GitLab CI templates? (Recommendation: Yes, in v1.0)
13. **Monitoring Integration:** Should NIC phone home telemetry (opt-in)? (Recommendation: v1.1, opt-in only)
14. **Marketplace Integration:** Package as AWS Marketplace / GCP Marketplace offering? (Recommendation: v2.0+)

---
