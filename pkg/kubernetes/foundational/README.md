# Foundational Services

This directory contains Argo CD Application manifests for foundational services that should be deployed on every Nebari cluster.

## Keycloak

Keycloak provides authentication and authorization services for the Nebari platform.

### Prerequisites

1. Ensure Argo CD is installed and running:
   ```bash
   kubectl get pods -n argocd
   ```

2. Create the keycloak namespace:
   ```bash
   kubectl apply -f keycloak-namespace.yaml
   ```

3. Create required secrets (DO NOT use the example passwords in production):
   ```bash
   # Create admin credentials
   kubectl create secret generic keycloak-admin-credentials \
     --from-literal=admin-password=<your-secure-password> \
     -n keycloak

   # Create PostgreSQL credentials
   kubectl create secret generic keycloak-postgresql-credentials \
     --from-literal=password=<your-secure-db-password> \
     -n keycloak
   ```

### Deployment

Deploy Keycloak using Argo CD:

```bash
kubectl apply -f keycloak-application.yaml
```

Check the deployment status:

```bash
# Check Argo CD application status
kubectl get application keycloak -n argocd

# Check Keycloak pods
kubectl get pods -n keycloak

# Check Keycloak service
kubectl get svc -n keycloak
```

### Accessing Keycloak

**Via Port-Forward (for testing):**
```bash
kubectl port-forward svc/keycloak -n keycloak 8080:80
```
Then visit: http://localhost:8080

**Via Ingress (production):**
Update the `hostname` in `keycloak-application.yaml` to match your domain, then access at:
- URL: https://keycloak.yourdomain.com
- Username: admin
- Password: (the password you set in the secret)

### Configuration

The Keycloak Helm chart is configured with:
- **Chart**: Bitnami Keycloak 24.3.0 (Keycloak 26.0.7)
- **Replicas**: 1 (can be scaled up for production)
- **Database**: PostgreSQL (included)
- **Proxy Mode**: Edge (for use behind ingress/load balancer)
- **Ingress**: Enabled with nginx ingress class
- **TLS**: Enabled with self-signed certificates (configure cert-manager for production)

### Customization

To customize Keycloak configuration, edit the `values` section in `keycloak-application.yaml`:

```yaml
spec:
  source:
    helm:
      values: |
        # Your custom values here
        replicaCount: 3
        # ... other values
```

Argo CD will automatically sync the changes.

### Troubleshooting

**Check Argo CD Application:**
```bash
kubectl describe application keycloak -n argocd
```

**Check Keycloak logs:**
```bash
kubectl logs -n keycloak -l app.kubernetes.io/name=keycloak
```

**Check PostgreSQL logs:**
```bash
kubectl logs -n keycloak -l app.kubernetes.io/name=postgresql
```

**Manual sync:**
```bash
# Via Argo CD CLI
argocd app sync keycloak

# Via kubectl
kubectl patch application keycloak -n argocd \
  --type merge \
  --patch '{"operation": {"sync": {"revision": "HEAD"}}}'
```

## Additional Foundational Services

Add more foundational service manifests here as needed:
- Cert-manager
- Ingress-nginx
- External-secrets
- Vault
- etc.
