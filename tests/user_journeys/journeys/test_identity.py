"""Identity journeys.

The realm is what every other Nebari login depends on, so these run
before any journey that assumes a working sign-in.
"""

from nebari_journeys import constants


def test_platform_promises_a_configured_nebari_realm(keycloak):
    """The realm exists and is enabled."""
    realm = keycloak.realm()
    assert realm["realm"] == constants.REALM_NAME
    assert realm["enabled"] is True


def test_realm_has_the_groups_the_platform_authorizes_against(keycloak):
    """longhorn-admins gates the Longhorn UI, so its absence is a silent
    authorization failure rather than an outage."""
    group_names = {g["name"] for g in keycloak.groups()}
    assert constants.LONGHORN_ADMINS_GROUP in group_names


def test_groups_are_a_default_client_scope(keycloak):
    """Without this, group claims never reach the token and every
    group-based authorization decision silently fails open or closed."""
    scope_names = {s["name"] for s in keycloak.realm_default_client_scopes()}
    assert "groups" in scope_names


def test_oidc_clients_exist_for_the_platform_uis(keycloak):
    for client_id in ("argocd", "longhorn"):
        assert keycloak.client(client_id) is not None, (
            f"OIDC client {client_id!r} missing from the {constants.REALM_NAME} realm"
        )


def test_argocd_client_redirects_to_this_platforms_domain(keycloak, platform_domain):
    client = keycloak.client("argocd")
    redirects = " ".join(client.get("redirectUris", []))
    assert platform_domain in redirects, (
        f"argocd client redirect URIs {redirects!r} do not mention {platform_domain}"
    )
