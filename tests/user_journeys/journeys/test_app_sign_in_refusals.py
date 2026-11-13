"""What sign-in must refuse.

The companion to test_app_sign_in.py: that file proves the right people get
in, this one proves the wrong requests do not. Each journey that expects a
refusal also checks the matching acceptance first, where one exists, so a
refusal caused by something unrelated being broken cannot pass as a
security check.

The gateway journeys cover every gateway-enforced NebariApp; the Nebi
journey needs nebi-pack and skips without it.
"""

import time
import warnings

import pytest

from nebari_journeys import apps
from nebari_journeys.keycloak import set_user_enabled, user_tokens
from nebari_journeys.ui import keycloak_refusal, login_via_keycloak, page_host

NEBI_PACK_APP = "nebi-pack"
NEBI_SESSION_PATH = "/api/v1/auth/session"

# The replay journey waits out one access token lifetime. Past this it is
# skipped rather than tying up a run for longer.
REPLAY_WAIT_LIMIT = 900

# Slack on top of the access token lifetime before replayed cookies must
# stop working: clock skew, plus Envoy's own refresh margin.
REPLAY_GRACE = 60


@pytest.fixture(scope="module")
def gateway_enforced_apps(cluster) -> list[apps.NebariApp]:
    found = [a for a in apps.nebari_apps(cluster) if a.enforce_at_gateway]
    if not found:
        pytest.skip("no NebariApp on this cluster leaves sign-in to the gateway")
    return [apps.wait_for_app_ready(cluster, a) for a in found]


@pytest.fixture(scope="module")
def keycloak_host(platform_domain, trust_anchor, dns_mapping, gateway_reachable):
    return apps.host_of(apps.advertised_issuer(platform_domain, trust_anchor or True))


@pytest.fixture
def signed_in(browser, browser_context_args, gateway_enforced_apps, scratch_user):
    """A browser context signed in to the first gateway-enforced app, with
    the app, its policy and the session cookies the gateway set."""
    app = gateway_enforced_apps[0]
    context = browser.new_context(**browser_context_args)
    page = context.new_page()
    login_via_keycloak(
        page,
        f"https://{app.hostname}/",
        scratch_user["username"],
        scratch_user["password"],
    )
    assert page_host(page.url) == app.hostname, f"sign-in did not complete: {page.url}"
    cookies = apps.cookies_for_host(context, app.hostname)
    assert apps.session_cookie_names(cookies), (
        f"signed in to {app.hostname}, but the gateway set no session cookies "
        f"(cookies: {sorted(cookies)})"
    )
    try:
        yield {"app": app, "context": context, "page": page, "cookies": cookies}
    finally:
        context.close()


def served(response) -> bool:
    return response.status_code < 300


def sent_to_keycloak(response, keycloak_host: str) -> bool:
    return (
        response.is_redirect
        and apps.host_of(response.headers.get("Location", "")) == keycloak_host
    )


@pytest.mark.ui
def test_a_wrong_password_never_reaches_a_gateway_enforced_app(
    browser, browser_context_args, gateway_enforced_apps, keycloak_host, scratch_user
):
    """Keycloak refuses the password and the gateway sets no session."""
    for app in gateway_enforced_apps:
        context = browser.new_context(**browser_context_args)
        try:
            page = context.new_page()
            error = keycloak_refusal(
                page,
                f"https://{app.hostname}/",
                scratch_user["username"],
                "not-the-password",
            )
            assert error, (
                f"{app.hostname}: Keycloak showed no error for a wrong password"
            )
            assert page_host(page.url) == keycloak_host, (
                f"{app.hostname}: left Keycloak after a wrong password: {page.url}"
            )
            leaked = apps.session_cookie_names(
                apps.cookies_for_host(context, app.hostname)
            )
            assert not leaked, (
                f"{app.hostname}: session cookies after a refused login: {leaked}"
            )
        finally:
            context.close()


@pytest.mark.ui
def test_a_forged_session_cookie_is_sent_back_to_keycloak(
    signed_in, keycloak_host, trust_anchor
):
    """Real cookies are served (the control); the same cookie names carrying
    made-up values are treated as no session at all."""
    app, cookies = signed_in["app"], signed_in["cookies"]
    url = f"https://{app.hostname}/"
    verify = trust_anchor or True

    genuine = apps.visit_with_cookies(url, cookies, verify)
    assert served(genuine), (
        f"the genuine session was not served: HTTP {genuine.status_code}"
    )

    forged = apps.visit_with_cookies(url, {name: "x" * 48 for name in cookies}, verify)
    assert sent_to_keycloak(forged, keycloak_host), (
        f"forged session cookies got HTTP {forged.status_code}, "
        f"Location {forged.headers.get('Location', '')[:80]!r}"
    )


def test_a_tampered_login_callback_creates_no_session(
    cluster, gateway_enforced_apps, trust_anchor, dns_mapping, gateway_reachable
):
    """A callback with an authorization code and state the gateway never
    issued must not start a session."""
    problems = []
    for app in gateway_enforced_apps:
        policy = apps.security_policies_for(cluster, app)[0]
        url = f"https://{app.hostname}{apps.callback_path(policy)}?code=forged&state=forged"
        response = apps.visit_with_cookies(url, {}, trust_anchor or True)
        set_cookies = apps.session_cookie_names(
            {c.name: c.value for c in response.cookies}
        )
        if served(response) and not response.is_redirect:
            problems.append(
                f"{app.hostname}: forged callback answered HTTP {response.status_code}"
            )
        if set_cookies:
            problems.append(
                f"{app.hostname}: forged callback set session cookies {set_cookies}"
            )
    assert not problems, "\n".join(problems)


@pytest.mark.ui
def test_signing_out_ends_the_session_in_the_browser(cluster, signed_in, keycloak_host):
    """Signing out clears the gateway session and the Keycloak session, so
    returning to the app asks for a password again."""
    app, page, context = signed_in["app"], signed_in["page"], signed_in["context"]
    path = apps.logout_path(apps.security_policies_for(cluster, app)[0])
    if not path:
        pytest.skip(f"{app.ref}'s SecurityPolicy defines no logout path")

    page.goto(f"https://{app.hostname}{path}")
    left = apps.session_cookie_names(apps.cookies_for_host(context, app.hostname))
    assert not left, f"session cookies survive signing out: {left}"

    page.goto(f"https://{app.hostname}/")
    page.wait_for_load_state()
    assert page_host(page.url) == keycloak_host, (
        f"after signing out, {app.hostname} was served again without a login: {page.url}"
    )
    assert page.locator("#username").count() == 1, (
        "after signing out, Keycloak signed the user straight back in: its "
        "session was not ended"
    )


@pytest.mark.ui
@pytest.mark.slow
def test_cookies_replayed_after_sign_out_stop_working_once_their_token_expires(
    cluster, keycloak, signed_in, keycloak_host, trust_anchor
):
    """Envoy's session cookies carry the tokens themselves, so a copy taken
    before sign-out keeps working until its access token expires. That is
    inherent to the design; what must hold is that it then STOPS working,
    because sign-out ended the Keycloak session that would let Envoy
    refresh it."""
    lifespan = int(keycloak.realm().get("accessTokenLifespan") or 300)
    if lifespan > REPLAY_WAIT_LIMIT:
        pytest.skip(f"access tokens live {lifespan}s; too long to wait out here")
    app, page, cookies = signed_in["app"], signed_in["page"], signed_in["cookies"]
    url = f"https://{app.hostname}/"
    verify = trust_anchor or True
    path = apps.logout_path(apps.security_policies_for(cluster, app)[0]) or "/logout"

    page.goto(f"https://{app.hostname}{path}")
    if served(apps.visit_with_cookies(url, cookies, verify)):
        warnings.warn(
            f"{app.hostname}: cookies copied before sign-out are still served right "
            f"after it; expected until the {lifespan}s access token expires",
            UserWarning,
            stacklevel=1,
        )
    time.sleep(lifespan + REPLAY_GRACE)
    replayed = apps.visit_with_cookies(url, cookies, verify)
    assert sent_to_keycloak(replayed, keycloak_host), (
        f"cookies copied before sign-out are still served {lifespan + REPLAY_GRACE}s "
        f"later (HTTP {replayed.status_code}): the session was refreshed after sign-out"
    )


@pytest.mark.ui
def test_a_disabled_user_cannot_sign_in(
    browser,
    browser_context_args,
    gateway_enforced_apps,
    keycloak,
    keycloak_host,
    scratch_user,
):
    app = gateway_enforced_apps[0]
    set_user_enabled(keycloak, scratch_user["id"], False)
    context = browser.new_context(**browser_context_args)
    try:
        page = context.new_page()
        error = keycloak_refusal(
            page,
            f"https://{app.hostname}/",
            scratch_user["username"],
            scratch_user["password"],
        )
        assert error, "Keycloak showed no error for a disabled user"
        assert page_host(page.url) == keycloak_host, (
            f"a disabled user left Keycloak: {page.url}"
        )
        leaked = apps.session_cookie_names(apps.cookies_for_host(context, app.hostname))
        assert not leaked, f"session cookies for a disabled user: {leaked}"
    finally:
        context.close()


def test_nebi_refuses_a_session_for_a_token_issued_to_another_app(
    cluster, keycloak, scratch_user, scratch_namespace
):
    """Nebi's session endpoint trades a Keycloak ID token for a Nebi session.
    It sits on Nebi's public /api/ route, so the gateway does not check it
    and Nebi's own verification is the only guard.

    Called in-cluster, exactly as JupyterHub calls it (same URL, same
    `IdToken` cookie). test_app_sign_in.py's token exchange journey is the
    positive control: there a genuine Nebi-audience token gets a session.
    Here a genuine token for another client, and the same token with its
    audience rewritten (signature no longer valid), must both be refused.
    """
    cluster.require_app(NEBI_PACK_APP)
    namespace = apps.destination_namespace(cluster, NEBI_PACK_APP)
    nebi = [
        a for a in apps.nebari_apps_in(cluster, namespace or "") if a.enforce_at_gateway
    ]
    assert len(nebi) == 1, f"expected one Nebi NebariApp in {namespace!r}"
    nebi_client = f"{nebi[0].namespace}-{nebi[0].name}"
    url = f"{nebi[0].in_cluster_url}{NEBI_SESSION_PATH}"

    token = user_tokens(keycloak, scratch_user["username"], scratch_user["password"])[
        "id_token"
    ]
    assert apps.jwt_claims(token).get("aud") != nebi_client

    scratch_namespace.run_pod("caller")
    scratch_namespace.wait_pod_ready("caller")
    for label, candidate in (
        ("a token issued to another client", token),
        ("a token with a forged audience", apps.with_audience(token, nebi_client)),
    ):
        out = scratch_namespace.exec(
            "caller",
            [
                "wget",
                "-q",
                "-S",
                "-O-",
                "-T",
                "10",
                "--header",
                f"Cookie: IdToken={candidate}",
                url,
            ],
        )
        assert "401 Unauthorized" in out or "403 Forbidden" in out, (
            f"Nebi did not refuse {label}; its answer:\n{out[-400:]}"
        )
