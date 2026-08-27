"""Constants mirrored from Go.

Every value here is pinned to its Go counterpart by
pkg/argocd/python_constants_test.go. Changing a value on either side
without the other fails the build. Do not add logic to this module.
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

# pkg/endpoint/endpoint.go
GATEWAY_NAMESPACE = "envoy-gateway-system"
GATEWAY_LABEL_SELECTOR = "gateway.envoyproxy.io/owning-gateway-name=nebari-gateway"

# pkg/config/config.go
GATEWAY_TLS_SECRET = "nebari-gateway-tls"

# pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml
GATEWAY_CERTIFICATE_NAME = "nebari-gateway-cert"

# pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml
REALM_NAME = "nebari"
LONGHORN_ADMINS_GROUP = "longhorn-admins"

# Owned by this suite; no Go counterpart.
JOURNEY_LABEL_KEY = "nebari.dev/test-journey"
JOURNEY_LABEL_VALUE = "true"
ARGOCD_NAMESPACE = "argocd"
