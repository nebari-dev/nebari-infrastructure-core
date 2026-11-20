# Do not rely on

These exist today but are not promised and may change without notice, so your pack must not depend on them.

- The shared Gateway's name and namespace.
- Envoy listener names, ports and pod labels.
- ClusterIssuer names other than `nebari-ca-issuer` (P18), including `selfsigned-issuer` and `letsencrypt-issuer`. `selfsigned-issuer` does not exist on Let's Encrypt clusters today.
- The Keycloak Service, namespace and admin Secrets.
- The master realm.
- Keycloak's bootstrap `admin` user, and the realm user `admin` in general. It collides with local `admin` users in packs such as Harbor and Artifact Keeper, and it is planned to change.
- Foundational app namespaces and Service names.
- Chart-generated resource names.
- Anything NIC creates for its own UIs.
