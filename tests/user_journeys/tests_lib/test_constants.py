"""Tests for the library itself. These need no cluster."""

import pytest

from nebari_journeys import constants


@pytest.mark.parametrize(
    "name,expected",
    [
        ("KEYCLOAK_NAMESPACE", "keycloak"),
        ("NEBARI_SYSTEM_NAMESPACE", "nebari-system"),
        ("LONGHORN_NAMESPACE", "longhorn-system"),
        ("KEYCLOAK_ADMIN_SECRET", "keycloak-admin-credentials"),
        ("KEYCLOAK_ADMIN_PASSWORD_KEY", "admin-password"),
        ("REALM_ADMIN_SECRET", "nebari-realm-admin-credentials"),
        ("REALM_ADMIN_PASSWORD_KEY", "password"),
        ("LONGHORN_OIDC_CLIENT_SECRET", "longhorn-oidc-client-secret"),
        ("PART_OF_LABEL", "app.kubernetes.io/part-of"),
        ("FOUNDATIONAL_PART_OF", "nebari-foundational"),
        ("GATEWAY_NAMESPACE", "envoy-gateway-system"),
        (
            "GATEWAY_LABEL_SELECTOR",
            "gateway.envoyproxy.io/owning-gateway-name=nebari-gateway",
        ),
        ("GATEWAY_TLS_SECRET", "nebari-gateway-tls"),
        ("REALM_NAME", "nebari"),
        ("LONGHORN_ADMINS_GROUP", "longhorn-admins"),
        ("JOURNEY_LABEL_KEY", "nebari.dev/test-journey"),
        ("JOURNEY_LABEL_VALUE", "true"),
    ],
)
def test_constant_has_expected_value(name, expected):
    assert getattr(constants, name) == expected
