# Foundational Software Stack

**(Identical to Native SDK edition - Section 9)**

The foundational software stack (Keycloak, LGTM, cert-manager, Envoy Gateway, ArgoCD) is deployed via ArgoCD applications, which are created by Terraform kubernetes_manifest resources. The deployment mechanism is the same, just initiated differently (Terraform vs Go SDK calls).

---
