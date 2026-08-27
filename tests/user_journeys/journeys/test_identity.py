"""Identity journeys.

The realm is what every other Nebari login depends on, so these run
before any journey that assumes a working sign-in.
"""

import pytest

from nebari_journeys import constants
from nebari_journeys.ui import is_access_denied, login_via_keycloak


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


@pytest.mark.ui
def test_a_new_user_can_sign_in_to_argocd_through_keycloak(
    page, platform_domain, scratch_user
):
    """The whole chain in one story: gateway, TLS, HTTPRoute, Keycloak,
    OIDC client configuration, and ArgoCD."""
    login_via_keycloak(
        page,
        f"https://argocd.{platform_domain}",
        scratch_user["username"],
        scratch_user["password"],
    )

    assert "login" not in page.url.lower(), (
        f"still on a login page after signing in: {page.url}"
    )


@pytest.mark.ui
def test_longhorn_ui_refuses_a_user_outside_the_admins_group(
    page, platform_domain, scratch_user
):
    """The SecurityPolicy and the Keycloak group claim mapping have to agree.
    When they drift, this fails open, which is a security problem rather than
    an outage, so nothing else would catch it.

    Denial detection here is PROVISIONAL. No live cluster was available to
    observe how Envoy's SecurityPolicy actually renders a denial for this
    platform: it may respond with a 401/403 status somewhere in the
    navigation chain, or it may return 200 with a redirect to an error page
    or a denial message rendered in the body. Both shapes are checked below
    so the first person to run this against a real cluster can see which
    one fires and tighten the assertion in one pass. There is no
    `page.context.last_status`; status has to come from the Response object
    `page.goto()` returns.

    `login_via_keycloak` performs its own internal navigation and, per its
    documented signature, returns None, so it cannot hand back the response
    from whatever redirect happens after a denied login. `status` below is
    therefore only the response to the *pre-login* request, which for a
    working OIDC flow is ordinarily a 200 (the Keycloak login page). If the
    platform denies access with an HTTP status rather than a rendered
    message, it most likely appears on the post-login redirect instead,
    which this cannot observe without a live cluster to confirm the shape
    against. The body-content check is the more load-bearing half of this
    assertion until that is confirmed.
    """
    response = page.goto(f"https://longhorn.{platform_domain}")
    login_via_keycloak(
        page,
        f"https://longhorn.{platform_domain}",
        scratch_user["username"],
        scratch_user["password"],
    )
    status = response.status if response is not None else None
    content = page.content().lower()

    denied = (status is not None and is_access_denied(status)) or "denied" in content
    assert denied, (
        f"a user outside {constants.LONGHORN_ADMINS_GROUP} reached the Longhorn UI "
        f"(final url={page.url!r}, status={status!r}, "
        f"body excerpt={content[:200]!r})"
    )


@pytest.mark.ui
def test_longhorn_ui_admits_a_user_in_the_admins_group(
    page, platform_domain, scratch_user, keycloak
):
    keycloak.add_user_to_group(scratch_user["id"], constants.LONGHORN_ADMINS_GROUP)

    login_via_keycloak(
        page,
        f"https://longhorn.{platform_domain}",
        scratch_user["username"],
        scratch_user["password"],
    )

    assert "denied" not in page.content().lower(), (
        f"a member of {constants.LONGHORN_ADMINS_GROUP} was refused the Longhorn UI"
    )
