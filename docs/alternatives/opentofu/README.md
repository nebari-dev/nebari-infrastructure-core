# OpenTofu Alternative Design

This directory contains an alternative design for Nebari Infrastructure Core that uses **OpenTofu/Terraform modules** with the **terraform-exec** Go library for infrastructure provisioning, as opposed to the main design which uses native cloud SDKs.

## Purpose

This alternative is provided for the development team to evaluate **before implementation begins**. Both approaches achieve the same end goal—deploying Nebari infrastructure—but differ significantly in their implementation strategy.

## Quick Comparison

| Aspect | Native SDK Design (Main) | OpenTofu Design (Alternative) |
|--------|--------------------------|-------------------------------|
| **Infrastructure Provisioning** | Direct cloud SDK calls (aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go) | OpenTofu/Terraform modules via terraform-exec |
| **State Management** | Stateless (queries cloud APIs) | Terraform state files (S3, GCS, Azure Blob) |
| **External Dependencies** | None (Go binary only) | Requires OpenTofu/Terraform binary |
| **Module Reuse** | Write custom provider code | Leverage existing Terraform module ecosystem |
| **Performance** | Fast (direct API calls) | Slower (plan/apply overhead) |
| **Debugging** | Direct SDK errors | Additional layer (Terraform → cloud API) |
| **Maturity** | New code, less proven | Battle-tested Terraform modules |
| **Learning Curve** | Learn cloud SDKs | Leverage existing Terraform knowledge |

## Strengths and Weaknesses

### Native SDK Design Strengths

1. **No external dependencies**: Single Go binary, no OpenTofu/Terraform installation required
2. **Stateless operation**: Queries actual cloud state on every run, no state drift issues
3. **Performance**: Direct API calls are faster than Terraform plan/apply cycles
4. **Error clarity**: Direct SDK errors are easier to debug and understand
5. **Fine-grained control**: Full control over cloud API interactions and error handling
6. **Simpler deployment**: No need to manage Terraform binary versions or state backends
7. **Resource discovery**: Tag-based discovery makes it easy to find NIC-managed resources

### Native SDK Design Weaknesses

1. **More code to write**: Must implement cloud API calls for each provider
2. **Less battle-tested**: New code without years of community validation
3. **No module ecosystem**: Cannot leverage existing Terraform modules
4. **Learning curve**: Team must learn cloud SDKs for each provider
5. **Implementation time**: Longer initial development time to build SDK integrations

### OpenTofu Design Strengths

1. **Proven modules**: Leverage thousands of existing, battle-tested Terraform modules
2. **Provider ecosystem**: All Terraform providers work with OpenTofu
3. **Community support**: Large community, well-documented patterns
4. **Faster initial development**: Reuse existing modules instead of writing SDK calls
5. **Familiar to teams**: Easier adoption for teams with Terraform experience
6. **Standard state format**: Terraform state is well-understood and tooling-rich
7. **Module composition**: Combine community modules with custom logic

### OpenTofu Design Weaknesses

1. **External dependency**: Requires OpenTofu/Terraform binary installation
2. **State management complexity**: Must manage state files, locking, and backends
3. **State drift**: Possible divergence between state file and actual infrastructure
4. **Performance overhead**: Terraform plan/apply cycles slower than direct API calls
5. **Debugging difficulty**: Additional abstraction layer between code and cloud APIs
6. **Binary compatibility**: Must ensure OpenTofu/Terraform version compatibility
7. **Error message indirection**: Terraform errors may obscure underlying API issues

## When to Choose Each Design

### Choose Native SDK Design If:

- You prioritize **zero external dependencies** (single binary deployment)
- You want **stateless operation** (no state file management)
- **Performance** is critical (direct API calls)
- You want **fine-grained control** over cloud interactions
- Team is comfortable learning cloud SDKs
- You prefer **simpler deployment** (no Terraform binary management)

### Choose OpenTofu Design If:

- You want to **leverage existing Terraform modules**
- Team has **strong Terraform expertise**
- **Faster initial development** is a priority
- You're comfortable with **state file management**
- You value **community-proven patterns**
- You want to **reuse existing Terraform code**

## Alternative Design Documents

The following documents describe the OpenTofu-specific aspects of the alternative design:

### Implementation Details

1. **[OpenTofu Module Architecture](implementation/05-opentofu-module-architecture.md)**
   - Module structure and organization
   - How OpenTofu modules are structured per provider
   - Module inputs, outputs, and dependencies

2. **[Terraform-Exec Integration](implementation/06-terraform-exec-integration.md)**
   - How Go code orchestrates OpenTofu via terraform-exec library
   - Command execution, output parsing, error handling
   - Working directory management

3. **[State Management](implementation/07-state-management.md)**
   - Terraform state backend configuration
   - State locking mechanisms
   - Handling state drift and conflicts

## Shared Design Elements

The following aspects are **identical** between both designs and are documented in the main `/docs` directory:

- Overall architecture and system goals
- Foundational software stack (ArgoCD, Keycloak, LGTM, cert-manager, Envoy Gateway)
- Nebari Operator design and CRD schemas
- Configuration file format (YAML)
- CLI commands and user experience
- Testing strategy (unit, integration, black box health tests)
- Observability approach (OpenTelemetry, LGTM stack)

## Decision Guidance

### Questions to Consider

1. **Team Expertise**: Does the team have more experience with Terraform or cloud SDKs?
2. **Deployment Model**: Is a single binary critical, or is managing Terraform acceptable?
3. **Performance Requirements**: How critical are fast deployment times?
4. **Module Reuse**: How valuable is access to the Terraform module ecosystem?
5. **Debugging Preference**: Does the team prefer direct SDK errors or is Terraform abstraction acceptable?
6. **State Management**: Is the team comfortable with stateless operation vs. Terraform state files?
7. **Long-term Maintenance**: Which approach will be easier to maintain over 3-5 years?

### Recommendation Process

1. **Review both designs** thoroughly with the entire team
2. **Prototype critical paths** in both approaches (e.g., AWS EKS cluster creation)
3. **Evaluate complexity** of each implementation
4. **Consider team skills** and hiring plans
5. **Assess long-term maintenance** burden
6. **Make decision** before implementation begins (avoid mixing approaches)

## Not a Hybrid Approach

**Important**: These are mutually exclusive design alternatives. NIC should use **either** native SDKs **or** OpenTofu, not both. Mixing approaches would add complexity without clear benefits.

## Status

- **Current Status**: Design alternatives for team evaluation
- **Next Step**: Team decision on which design to implement
- **Timeline**: Decision should be made before v0.2.0 implementation begins

---

**Questions or Feedback**: Open GitHub issues with the tag `design-decision` to discuss trade-offs.
