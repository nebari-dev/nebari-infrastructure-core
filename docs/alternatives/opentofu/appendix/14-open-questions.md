# Open Questions

### 14.1 Technical Questions

1. **OpenTofu vs Terraform:** Stick with OpenTofu or support both? (Recommendation: OpenTofu primary, Terraform compatible)
2. **Module Source:** Vendor modules in repo or use remote sources? (Recommendation: Vendor for stability)
3. **State Backend:** Require state backend config or default to local? (Recommendation: Require for production, allow local for testing)
4. **Terraform Version:** Pin specific version or allow range? (Recommendation: Pin minor version, allow patch updates)
5. **Plan Output:** Show full Terraform plan to user or summarize? (Recommendation: Summarize by default, --verbose for full plan)

### 14.2 Module Questions

6. **Community Modules:** Use as-is or fork and customize? (Recommendation: Use as-is initially, fork if needed)
7. **Module Versioning:** How to handle module version updates? (Recommendation: Test in dev, gradual rollout)
8. **Module Testing:** Test all modules or just custom ones? (Recommendation: Test all, especially custom)

### 14.3 Deployment Questions

9. **Concurrent Deployments:** Allow multiple `nic deploy` in parallel? (Recommendation: No, rely on Terraform state locking)
10. **Partial Apply:** Support applying only specific modules? (Recommendation: v2.0+, use `terraform apply -target`)
11. **Rollback:** How to rollback failed deployments? (Recommendation: `terraform state` commands + redeploy previous config)

### 14.4 Integration Questions

12. **CI/CD Integration:** Should NIC provide Terraform Cloud/Atlantis integration? (Recommendation: v1.1)
13. **State Migration:** Provide tools to migrate from old Terraform to NIC Terraform modules? (Recommendation: v1.1)
14. **Terraform Lock File:** Commit `.terraform.lock.hcl` to repo? (Recommendation: Yes, for reproducibility)

---
