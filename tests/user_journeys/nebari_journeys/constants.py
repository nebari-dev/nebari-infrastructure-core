"""Constants mirrored from Go.

Every value here is pinned to its Go counterpart by
pkg/argocd/python_constants_test.go. Changing a value on either side
without the other fails the build. Do not add logic to this module.

A value that mirrors a Go constant, a Go string template, or a manifest
template belongs here and nowhere else, so that the Go contract test can
see it. A value the suite invents for itself is listed at the bottom and
marked exempt in that test.
"""

# pkg/argocd/foundational.go
KEYCLOAK_NAMESPACE = "keycloak"
NEBARI_SYSTEM_NAMESPACE = "nebari-system"
LONGHORN_NAMESPACE = "longhorn-system"
KEYCLOAK_ADMIN_SECRET = "keycloak-admin-credentials"
KEYCLOAK_ADMIN_PASSWORD_KEY = "admin-password"
REALM_ADMIN_SECRET = "nebari-realm-admin-credentials"
REALM_ADMIN_PASSWORD_KEY = "password"
LONGHORN_OIDC_CLIENT_SECRET = "longhorn-oidc-client-secret"
PART_OF_LABEL = "app.kubernetes.io/part-of"
FOUNDATIONAL_PART_OF = "nebari-foundational"

# pkg/argocd/config.go
ARGOCD_NAMESPACE = "argocd"

# pkg/endpoint/endpoint.go
GATEWAY_NAMESPACE = "envoy-gateway-system"
GATEWAY_LABEL_SELECTOR = "gateway.envoyproxy.io/owning-gateway-name=nebari-gateway"

# pkg/config/config.go
GATEWAY_TLS_SECRET = "nebari-gateway-tls"

# pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml
GATEWAY_CERTIFICATE_NAME = "nebari-gateway-cert"

# pkg/argocd/templates/manifests/networking/gateway.yaml
GATEWAY_NAME = "nebari-gateway"

# pkg/argocd/bootstrap.go, rootAppOfAppsTemplate
ROOT_APP_NAME = "nebari-root"

# pkg/argocd/templates/apps/longhorn-backup.yaml
LONGHORN_BACKUP_APP = "longhorn-backup"

# pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml
REALM_NAME = "nebari"
ARGOCD_ADMINS_GROUP = "argocd-admins"
ARGOCD_VIEWERS_GROUP = "argocd-viewers"
LONGHORN_ADMINS_GROUP = "longhorn-admins"
ARGOCD_OIDC_CLIENT = "argocd"
LONGHORN_OIDC_CLIENT = "longhorn"

# Owned by this suite; no Go counterpart.
JOURNEY_LABEL_KEY = "nebari.dev/test-journey"
JOURNEY_LABEL_VALUE = "true"
