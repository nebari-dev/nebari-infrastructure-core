# Open Questions

### 13.1 Technical Questions

1. **Resolved:** Using OpenTofu with terraform-exec orchestration and standard Terraform state management
2. **Multi-Cluster:** How to manage multiple clusters in one state file? (Options: separate states, or cluster array in state)
3. **Custom Kubernetes Distributions:** Support for k0s, k3d, RKE2? (v1: No, v2: Maybe)
4. **Helm Chart Storage:** Where to store foundational software Helm charts? (OCI registry? Git?)
5. **Operator HA:** Should operator run in HA mode (multiple replicas)? (Recommendation: Yes, with leader election)

### 13.2 Configuration Questions

6. **Config Validation:** Schema validation via JSON Schema or custom Go validation? (Recommendation: Custom Go + JSON Schema for IDE support)
7. **Config Inheritance:** Support for base + overlay configs? (Recommendation: No for MVP, Yes in future versions via `extends` field)
8. **Secrets Management:** How to handle secrets in config (Keycloak admin password, etc.)? (Options: external secrets operator, sealed secrets, cloud secrets manager)

### 13.3 Deployment Questions

9. **Rollback Strategy:** Should `nic rollback` be a command? (Recommendation: Yes, Phase 2)
10. **Blue/Green Deployments:** Support for blue/green cluster deployments? (Recommendation: Future)
11. **Canary Deployments:** For foundational software updates? (Recommendation: Future)

### 13.4 Integration Questions

12. **CI/CD Integration:** Should NIC provide GitHub Actions / GitLab CI templates? (Recommendation: Yes, Phase 2)
13. **Monitoring Integration:** Should NIC phone home telemetry (opt-in)? (Recommendation: Phase 2, opt-in only)
14. **Marketplace Integration:** Package as AWS Marketplace / GCP Marketplace offering? (Recommendation: Future)

### 13.5 Platform Automation Questions

15. **Git Repository Provisioning:** Should NIC automatically provision Git repositories and setup CI/CD workflows for infrastructure changes?

    - **Use Case:** `nic init` creates GitHub repo, adds config.yaml, sets up GitHub Actions/GitLab CI for automated infrastructure updates
    - **Providers:** GitHub, GitLab, Gitea (self-hosted)
    - **Features:** Branch protection, PR-based workflow, automated validation, auto-apply on merge
    - **Recommendation:** Phase 2, start with GitHub integration

16. **CI/CD Workflow Generation:** Should NIC auto-generate and manage CI/CD pipelines for infrastructure automation?
    - **Workflows:**
      - PR validation: `nic validate` + `nic plan` on every PR
      - Auto-deploy: `nic deploy` on merge to main (with approval gates)
      - Scheduled drift detection: Daily `nic status` to detect manual changes
      - Automated testing: Integration tests before deployment
    - **Customization:** Template-based with user overrides
    - **Recommendation:** Phase 2, essential for GitOps workflow

### 13.6 Application Stack Questions

17. **Software Stack Specification:** Should NIC support declarative specifications for complete software stacks (databases, message queues, caching, etc.) deployable on top of foundational software?

    - **Use Case:** Define entire platform + applications in single config.yaml
    - **Example Stacks:**
      - Data Science: PostgreSQL + Redis + MinIO + JupyterHub + Dask
      - ML Platform: MLflow + Kubeflow + Model Registry + Feature Store
      - Web Platform: PostgreSQL + Redis + RabbitMQ + Object Storage
    - **Integration:** Via Helm chart repositories, ArgoCD ApplicationSets
    - **Recommendation:** Phase 2, using Helm chart catalogs and pre-defined stack templates

18. **Full Stack in One Repo:** Should users be able to define foundational software + application stacks + configuration in a single repository?

    - **Structure:**

      ```
      nebari-deployment/
      ├── config.yaml          # Platform + stacks
      ├── stacks/
      │   ├── postgresql-values.yaml  # DB config
      │   ├── jupyterhub-values.yaml  # App config
      │   └── dask-values.yaml        # Compute config
      ├── policies/                    # OPA policies
      └── .github/workflows/           # Auto-generated CI/CD
      ```

    - **Benefits:** Single source of truth, version controlled, auditable, reproducible
    - **Recommendation:** Phase 2, core feature for platform teams

19. **Stack Templates & Marketplace:** Should NIC provide pre-built stack templates (data science, ML, web app) and a marketplace for community stacks?
    - **Built-in Templates:**
      - nebari-data-science-stack
      - nebari-ml-platform-stack
      - nebari-web-platform-stack
    - **Community Marketplace:** GitHub-based registry of vetted stack configurations
    - **Recommendation:** Phase 2 for templates, Future for marketplace

---
