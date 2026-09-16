"""Identity journeys.

The realm is what every other Nebari login depends on, so these run
before any journey that assumes a working sign-in.

Longhorn's realm objects - the `longhorn` OIDC client, the
`longhorn-admins` group and its membership - are created inside a
conditional block guarded by `[ -z "$LONGHORN_CLIENT_SECRET" ]` in
pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml. On a Nebari
cluster deployed without Longhorn they do not exist, and their absence is
not a failure. Each Longhorn-specific assertion is therefore gated on
`cluster.has_longhorn()`, and the two browser journeys skip outright, while
the ArgoCD half of every shared check still runs.
"""

import pytest

from nebari_journeys import constants
from nebari_journeys.keycloak import redirect_hosts
from nebari_journeys.ui import (
    ARGOCD_OIDC_LOGIN_PATH,
    LONGHORN_UI_MARKER,
    is_access_denied,
    login_via_keycloak,
    page_host,
)


def test_platform_promises_a_configured_nebari_realm(keycloak):
    """The realm exists and is enabled."""
    realm = keycloak.realm()
    assert realm["realm"] == constants.REALM_NAME
    assert realm["enabled"] is True


def test_realm_has_the_groups_the_platform_authorizes_against(cluster, keycloak):
    """These groups gate the platform UIs, so an absent one is a silent
    authorization failure rather than an outage.

    The ArgoCD groups are unconditional. longhorn-admins is only asserted on
    a cluster that has Longhorn, so the ArgoCD half of this check still runs
    everywhere.
    """
    group_names = {g["name"] for g in keycloak.groups()}

    for expected in (constants.ARGOCD_ADMINS_GROUP, constants.ARGOCD_VIEWERS_GROUP):
        assert expected in group_names, (
            f"group {expected!r} missing from the {constants.REALM_NAME} realm "
            f"(found: {sorted(group_names)})"
        )

    if not cluster.has_longhorn():
        pytest.skip(
            f"Longhorn is not installed, so {constants.LONGHORN_ADMINS_GROUP!r} is "
            "not expected; the ArgoCD groups were checked"
        )

    assert constants.LONGHORN_ADMINS_GROUP in group_names, (
        f"group {constants.LONGHORN_ADMINS_GROUP!r} missing from the "
        f"{constants.REALM_NAME} realm although Longhorn is installed "
        f"(found: {sorted(group_names)})"
    )


def test_groups_are_a_default_client_scope(keycloak):
    """Without this, group claims never reach the token and every
    group-based authorization decision silently fails open or closed."""
    scope_names = {s["name"] for s in keycloak.realm_default_client_scopes()}
    assert "groups" in scope_names


def test_oidc_clients_exist_for_the_platform_uis(cluster, keycloak):
    """The ArgoCD client is unconditional; the Longhorn client only exists on
    a cluster that has Longhorn."""
    assert keycloak.client(constants.ARGOCD_OIDC_CLIENT) is not None, (
        f"OIDC client {constants.ARGOCD_OIDC_CLIENT!r} missing from the "
        f"{constants.REALM_NAME} realm"
    )

    if not cluster.has_longhorn():
        pytest.skip(
            f"Longhorn is not installed, so the {constants.LONGHORN_OIDC_CLIENT!r} "
            "OIDC client is not expected; the ArgoCD client was checked"
        )

    assert keycloak.client(constants.LONGHORN_OIDC_CLIENT) is not None, (
        f"OIDC client {constants.LONGHORN_OIDC_CLIENT!r} missing from the "
        f"{constants.REALM_NAME} realm although Longhorn is installed"
    )


def test_argocd_client_redirects_to_this_platforms_domain(keycloak, platform_domain):
    """Compares HOSTS, not substrings.

    This asserted `platform_domain in " ".join(redirectUris)` first, which
    is a weaker check than its name: it passes on
    `https://argocd.nebari.test.somewhere-else.example/*`, and passes when
    the domain shows up only in an unrelated client's URI. The thing worth
    knowing is that a redirect URI points at THIS platform's ArgoCD host.
    """
    client = keycloak.client(constants.ARGOCD_OIDC_CLIENT)
    expected_host = f"{constants.ARGOCD_OIDC_CLIENT}.{platform_domain}".lower()
    hosts = redirect_hosts(client)
    assert expected_host in hosts, (
        f"argocd client redirect URIs point at {sorted(hosts)}, none of which is "
        f"{expected_host!r} (raw URIs: {client.get('redirectUris')!r})"
    )


@pytest.mark.ui
@pytest.mark.requires_trusted_ca
def test_a_new_user_can_sign_in_to_argocd_through_keycloak(
    page, platform_domain, scratch_user
):
    """The whole chain in one story: gateway, TLS, HTTPRoute, Keycloak,
    OIDC client configuration, and ArgoCD.

    Marked `requires_trusted_ca` and skipped (not failed) on a cluster whose
    gateway anchor is a self-signed leaf: ArgoCD's server performs OIDC
    discovery against the external Keycloak URL and cannot trust that
    certificate, so this journey cannot pass for a reason that has nothing
    to do with the realm, the client, or ArgoCD's configuration (issue #490,
    root cause #447; issue #607 is a second, separate blocker on the same
    cluster shape). See `journeys/conftest.py`'s
    `skip_trusted_ca_marked_tests_on_self_signed` fixture. This is narrower
    than the `tls` marker: the other identity journeys talk to Keycloak
    directly from the test runner over a trust anchor the suite derives, so
    they are unaffected and keep running.

    Navigates to ARGOCD_OIDC_LOGIN_PATH rather than the bare host. Argo CD's
    own /login page does not auto-redirect to the identity provider: it
    renders Argo CD's LOCAL username/password form alongside a separate "LOG
    IN VIA <provider>" button. Filling the Keycloak selectors there either
    times out or, worse, submits a Keycloak user to Argo CD's local login,
    which is not what this journey claims to verify. See the comment on
    ARGOCD_OIDC_LOGIN_PATH: the path is provisional and needs confirming on
    the first run against a live cluster.
    """
    argocd_host = f"argocd.{platform_domain}"
    keycloak_host = f"keycloak.{platform_domain}"

    login_via_keycloak(
        page,
        f"https://{argocd_host}{ARGOCD_OIDC_LOGIN_PATH}",
        scratch_user["username"],
        scratch_user["password"],
    )

    final_host = page_host(page.url)
    assert final_host != keycloak_host, (
        "the OIDC round trip did not complete: the browser is still on Keycloak "
        f"after submitting the login form (url={page.url!r}). The user was not "
        "signed in to ArgoCD, so this says nothing about ArgoCD itself."
    )
    assert final_host == argocd_host, (
        f"after signing in the browser ended up on {final_host!r}, not "
        f"{argocd_host!r} (url={page.url!r})"
    )
    assert "login" not in page.url.lower(), (
        f"still on a login page after signing in: {page.url}"
    )


@pytest.mark.ui
def test_longhorn_ui_refuses_a_user_outside_the_admins_group(
    cluster, page, platform_domain, scratch_user
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

    Marker absence ALONE is not enough, and that is why the host assertions
    run first. Any outcome that is not a rendered Longhorn UI satisfies
    "marker absent", including outcomes where the test never tested anything:
    Keycloak rejecting the password, a misconfigured OIDC client, or a
    gateway 502 all leave an error page with no marker on it. Asserting that
    the browser came back to the Longhorn host, and is not sitting on
    Keycloak, establishes that the OIDC round trip actually completed before
    absence of the marker is allowed to mean "access was denied". The two
    failure modes ("auth never happened" and "the user was wrongly admitted")
    have separate messages so a CI log distinguishes them.

    Only the exact marker STRING in `LONGHORN_UI_MARKER` is provisional and
    needs confirming against a live Longhorn UI on first run; the detection
    logic itself is sound regardless of that string.

    `status` is NOT part of the pass/fail decision: `login_via_keycloak`
    returns None and performs its own internal navigation, so `status`
    below only ever reflects the pre-login response (ordinarily 200, the
    Keycloak login page), not whatever happens on any post-login redirect.
    It is structurally incapable of independently signalling denial. It is
    kept only as extra diagnostic evidence in the failure message. There is
    no `page.context.last_status`; the Response comes from `page.goto()`.
    """
    cluster.require_longhorn()
    longhorn_host = f"longhorn.{platform_domain}"
    keycloak_host = f"keycloak.{platform_domain}"

    response = page.goto(f"https://{longhorn_host}")
    login_via_keycloak(
        page,
        f"https://{longhorn_host}",
        scratch_user["username"],
        scratch_user["password"],
    )
    status = response.status if response is not None else None
    final_host = page_host(page.url)

    assert final_host != keycloak_host, (
        "authentication never completed: the browser is still on Keycloak after "
        f"submitting the login form (url={page.url!r}). Nothing about Longhorn "
        "authorization was tested, so this is NOT evidence that access was denied."
    )
    assert final_host == longhorn_host, (
        "authentication did not return to Longhorn: the browser ended up on "
        f"{final_host!r} (url={page.url!r}). Nothing about Longhorn authorization "
        "was tested, so this is NOT evidence that access was denied."
    )

    visible_text = page.inner_text("body")
    assert LONGHORN_UI_MARKER not in visible_text, (
        f"a user outside {constants.LONGHORN_ADMINS_GROUP} reached the Longhorn UI "
        f"(final url={page.url!r}, pre-login status={status!r} "
        f"[diagnostic only, likely denied={is_access_denied(status) if status else 'n/a'}], "
        f"body excerpt={visible_text[:200]!r})"
    )


@pytest.mark.ui
def test_longhorn_ui_admits_a_user_in_the_admins_group(
    cluster, page, platform_domain, scratch_user, keycloak
):
    """Asserts the POSITIVE marker is present, so a failed login, a stuck
    Keycloak page, or a network error page -- none of which contain
    "denied" either -- now fail this test instead of silently passing it.
    See the sibling denial test for why a positive marker was chosen over
    a substring search, and why the marker string needs confirming against
    a live Longhorn UI on first run.
    """
    cluster.require_longhorn()
    longhorn_host = f"longhorn.{platform_domain}"

    keycloak.add_user_to_group(scratch_user["id"], constants.LONGHORN_ADMINS_GROUP)

    login_via_keycloak(
        page,
        f"https://{longhorn_host}",
        scratch_user["username"],
        scratch_user["password"],
    )

    visible_text = page.inner_text("body")
    assert LONGHORN_UI_MARKER in visible_text, (
        f"a member of {constants.LONGHORN_ADMINS_GROUP} was refused the Longhorn UI "
        f"(final url={page.url!r}, body excerpt={visible_text[:200]!r})"
    )
