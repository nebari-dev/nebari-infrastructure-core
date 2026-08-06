# Keycloak `groups` claim format contract

Every OIDC client in the `nebari` realm shares one `groups` client scope, and
its group-membership mapper is configured with `full.path=true`. That single
setting is a realm-wide contract:

- **Tokens always carry group *paths*, not bare names.** A user in the
  `longhorn-admins` group gets `"groups": ["/longhorn-admins"]` in their JWT,
  not `["longhorn-admins"]`.
- **Who owns the setting:** the data-science-pack chart's RBAC bootstrap job
  (`files/keycloak_rbac_bootstrap.py`) reconciles the mapper to
  `full.path=true` on every sync, because its group-lookup-by-path logic needs
  the path form. Anything else that flips the mapper gets overwritten on the
  next sync.
- **What NIC does:** `realm-setup-job.yaml` creates the mapper with
  `full.path=true` as a best-effort default when the scope is brand new
  (`kcadm create ... || true` cannot change an existing mapper), and otherwise
  leaves it alone.
- **What consumers must do:** anything matching on the `groups` claim must
  match the path form (leading slash). Current consumers:
  - ArgoCD RBAC (`pkg/argocd/config.go`) — must match both bare and path forms
    for compatibility with realms created before this contract (see
    [#385](https://github.com/nebari-dev/nebari-infrastructure-core/pull/385)).
  - The Envoy Gateway SecurityPolicy protecting the Longhorn UI
    (`pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml`)
    — matches `/longhorn-admins`.

If you add a new consumer of the `groups` claim, match the path form. If you
need the bare form, do not change the mapper — it will be reconciled back.
