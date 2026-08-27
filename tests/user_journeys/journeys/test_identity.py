"""Identity journeys.

The realm is what every other Nebari login depends on, so these run
before any journey that assumes a working sign-in.
"""

import pytest

from nebari_journeys import constants
from nebari_journeys.ui import LONGHORN_UI_MARKER, is_access_denied, login_via_keycloak


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

    Detection is keyed off a POSITIVE marker of the Longhorn UI having
    rendered (`LONGHORN_UI_MARKER`), not off guessing the wording of a
    denial. A substring search for "denied" in `page.content()` (raw HTML,
    including bundled JavaScript) was tried first and rejected: Longhorn's
    SPA bundle can plausibly contain that word in its own client-side error
    handling even when the page renders normally for an admitted user, so
    that check could pass while access was in fact granted -- exactly the
    fail-open this journey exists to catch. Asserting the marker's ABSENCE
    means a fail-open (the UI actually rendering) makes the marker appear
    and the test correctly fails.

    The marker is matched against `page.inner_text("body")` (visible
    rendered text), not raw HTML, so a string sitting in an unrendered
    script bundle can never satisfy it.

    Only the exact marker STRING in `LONGHORN_UI_MARKER` is provisional and
    needs confirming against a live Longhorn UI on first run; the detection
    logic itself (assert absence of a real positive signal) is sound
    regardless of that string.

    `status` is NOT part of the pass/fail decision: `login_via_keycloak`
    returns None and performs its own internal navigation, so `status`
    below only ever reflects the pre-login response (ordinarily 200, the
    Keycloak login page), not whatever happens on any post-login redirect.
    It is structurally incapable of independently signalling denial. It is
    kept only as extra diagnostic evidence in the failure message. There is
    no `page.context.last_status`; the Response comes from `page.goto()`.
    """
    response = page.goto(f"https://longhorn.{platform_domain}")
    login_via_keycloak(
        page,
        f"https://longhorn.{platform_domain}",
        scratch_user["username"],
        scratch_user["password"],
    )
    status = response.status if response is not None else None
    visible_text = page.inner_text("body")

    assert LONGHORN_UI_MARKER not in visible_text, (
        f"a user outside {constants.LONGHORN_ADMINS_GROUP} reached the Longhorn UI "
        f"(final url={page.url!r}, pre-login status={status!r} "
        f"[diagnostic only, likely denied={is_access_denied(status) if status else 'n/a'}], "
        f"body excerpt={visible_text[:200]!r})"
    )


@pytest.mark.ui
def test_longhorn_ui_admits_a_user_in_the_admins_group(
    page, platform_domain, scratch_user, keycloak
):
    """Asserts the POSITIVE marker is present, so a failed login, a stuck
    Keycloak page, or a network error page -- none of which contain
    "denied" either -- now fail this test instead of silently passing it.
    See the sibling denial test for why a positive marker was chosen over
    a substring search, and why the marker string needs confirming against
    a live Longhorn UI on first run.
    """
    keycloak.add_user_to_group(scratch_user["id"], constants.LONGHORN_ADMINS_GROUP)

    login_via_keycloak(
        page,
        f"https://longhorn.{platform_domain}",
        scratch_user["username"],
        scratch_user["password"],
    )

    visible_text = page.inner_text("body")
    assert LONGHORN_UI_MARKER in visible_text, (
        f"a member of {constants.LONGHORN_ADMINS_GROUP} was refused the Longhorn UI "
        f"(final url={page.url!r}, body excerpt={visible_text[:200]!r})"
    )
