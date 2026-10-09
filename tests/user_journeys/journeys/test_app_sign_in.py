"""Sign-in journeys for the apps packs install.

Every pack UI is a NebariApp, and the nebari-operator decides how a user
signs in to it. Most apps leave that to the gateway: the operator writes an
Envoy Gateway SecurityPolicy that sends any visitor without a session to
Keycloak first. Some, JupyterHub among them, set `enforceAtGateway: false`
and run their own Keycloak login.

The gateway journeys are generic: they cover every gateway-enforced
NebariApp on the cluster, whichever packs installed them, and skip only
when there is none. The JupyterHub journeys need data-science-pack (and,
for the token exchange, nebi-pack) and skip without them.

The first two journeys guard a specific regression. Envoy Gateway v1.9.1
rejects a SecurityPolicy whose OIDC issuer is not https. An operator that
derived the issuer from Keycloak's in-cluster http URL had every policy
rejected, which left gateway-enforced apps with no gateway login at all.
"""

from datetime import UTC, datetime

import pytest

from nebari_journeys import apps
from nebari_journeys.ui import (
    approve_hub_oauth_consent,
    follow_login_redirects,
    login_via_keycloak,
    page_host,
    wait_for_cookie,
)

DATA_SCIENCE_PACK_APP = "data-science-pack"
NEBI_PACK_APP = "nebi-pack"

# z2jh's hub pod.
HUB_POD_SELECTOR = "component=hub"

# jhub-apps is a hub service with its own OAuth session: its API takes a
# jhub_apps_access_token cookie that this login round trip with the hub sets,
# as the jhub-apps UI does when it loads.
JHUB_APPS_LOGIN_PATH = "/services/japps/jhub-login"
JHUB_APPS_SESSION_COOKIE = "jhub_apps_access_token"

# jhub-apps fills its environment selector from this endpoint. On a hub with
# nebi-pack, answering it runs the hub -> Keycloak -> Nebi token exchange.
JHUB_APPS_ENVIRONMENTS_PATH = "/services/japps/conda-environments/"

# Lines data-science-pack's hub logs while listing a user's Nebi
# environments (config/jupyterhub/03-nebi-envs.py and the shared exchange in
# 01-spawner.py there). The first proves the exchange reached Nebi; the rest
# name each way it can fail.
ENVIRONMENTS_LISTED = "nebi-envs: listed"
EXCHANGE_FAILURES = (
    "token exchange returned no JWT",
    "token-exchange: aborting",
    "nebi-envs: no auth_state",
    "nebi-envs: failed to fetch workspaces",
)


@pytest.fixture(scope="module")
def gateway_enforced_apps(cluster) -> list[apps.NebariApp]:
    found = [a for a in apps.nebari_apps(cluster) if a.enforce_at_gateway]
    if not found:
        pytest.skip(
            "no NebariApp on this cluster leaves sign-in to the gateway, so "
            "there is no SecurityPolicy to check"
        )
    return [apps.wait_for_app_ready(cluster, a) for a in found]


@pytest.fixture(scope="module")
def keycloak_issuer(platform_domain, trust_anchor, dns_mapping, gateway_reachable):
    return apps.advertised_issuer(platform_domain, trust_anchor or True)


def test_every_gateway_enforced_app_has_an_accepted_security_policy(
    cluster, gateway_enforced_apps
):
    """A policy the gateway rejected is invisible to users until they find
    the app open, or unreachable. Every gateway-enforced app needs exactly
    one policy, accepted by every Gateway it attaches to.

    The operator must also report the app's auth as reconciled. An accepted
    policy alone is not enough: after an upgrade, a policy written by an
    earlier operator stays in place and stays accepted while the current
    operator fails to update it, which is exactly how an operator emitting
    an issuer Envoy Gateway rejects looked on an upgraded cluster."""
    problems = []
    for app in gateway_enforced_apps:
        auth = app.conditions.get("AuthReady") or {}
        if auth.get("status") != "True":
            problems.append(
                f"{app.ref}: operator reports AuthReady={auth.get('status')} "
                f"({auth.get('reason')}: {(auth.get('message') or '')[:300]})"
            )
        policies = apps.security_policies_for(cluster, app)
        if len(policies) != 1:
            problems.append(f"{app.ref}: {len(policies)} SecurityPolicies, expected 1")
            continue
        accepted = apps.acceptance(policies[0])
        if not accepted:
            problems.append(f"{app.ref}: SecurityPolicy has no Accepted condition yet")
        for condition in accepted:
            if condition.get("status") != "True":
                problems.append(
                    f"{app.ref}: SecurityPolicy not accepted "
                    f"({condition.get('reason')}: {condition.get('message')})"
                )
    assert not problems, "\n".join(problems)


def test_gateway_logins_trust_keycloaks_public_https_issuer(
    cluster, gateway_enforced_apps, keycloak_issuer
):
    """The issuer a policy names must be the one Keycloak advertises, and
    https: Envoy Gateway v1.9.1 rejects anything else. The browser is sent
    to the authorization endpoint, so that must be a URL a browser can
    reach: https, on Keycloak's public host."""
    keycloak_host = apps.host_of(keycloak_issuer)
    problems = []
    for app in gateway_enforced_apps:
        for policy in apps.security_policies_for(cluster, app):
            provider = apps.oidc_provider(policy)
            issuer = provider.get("issuer", "")
            if issuer != keycloak_issuer:
                problems.append(
                    f"{app.ref}: issuer {issuer!r}, Keycloak advertises {keycloak_issuer!r}"
                )
            authorize = provider.get("authorizationEndpoint")
            if authorize and not (
                authorize.startswith("https://")
                and apps.host_of(authorize) == keycloak_host
            ):
                problems.append(
                    f"{app.ref}: browsers are sent to {authorize!r}, which is not "
                    f"https on {keycloak_host}"
                )
    assert keycloak_issuer.startswith("https://"), (
        f"Keycloak advertises a non-https issuer {keycloak_issuer!r}"
    )
    assert not problems, "\n".join(problems)


def test_gateway_sends_a_visitor_without_a_session_to_keycloak(
    gateway_enforced_apps, keycloak_issuer, trust_anchor, dns_mapping, gateway_reachable
):
    """What a policy is for: the gateway answers an anonymous request with
    a redirect to Keycloak, instead of serving the app."""
    keycloak_host = apps.host_of(keycloak_issuer)
    problems = []
    for app in gateway_enforced_apps:
        response = apps.anonymous_visit(
            f"https://{app.hostname}/", trust_anchor or True
        )
        location = response.headers.get("Location", "")
        if not response.is_redirect or apps.host_of(location) != keycloak_host:
            problems.append(
                f"{app.hostname}: HTTP {response.status_code}, "
                f"Location {location[:120]!r}; expected a redirect to {keycloak_host}"
            )
    assert not problems, "\n".join(problems)


@pytest.mark.ui
def test_a_new_user_can_sign_in_to_every_gateway_enforced_app(
    browser, browser_context_args, gateway_enforced_apps, keycloak_issuer, scratch_user
):
    """The whole gateway login, as a user does it: open the app, land on
    Keycloak, sign in, end up back on the app with it rendered.

    One fresh browser context per app, so each sign-in starts with no
    Keycloak session and really shows the login form.
    """
    keycloak_host = apps.host_of(keycloak_issuer)
    problems = []
    for app in gateway_enforced_apps:
        context = browser.new_context(**browser_context_args)
        try:
            page = context.new_page()
            login_via_keycloak(
                page,
                f"https://{app.hostname}/",
                scratch_user["username"],
                scratch_user["password"],
            )
            landed = page_host(page.url)
            if landed == keycloak_host:
                problems.append(
                    f"{app.hostname}: still on Keycloak after signing in ({page.url})"
                )
            elif landed != app.hostname:
                problems.append(f"{app.hostname}: ended up on {page.url}")
            else:
                status = page.evaluate(
                    "() => performance.getEntriesByType('navigation')[0]?.responseStatus ?? 0"
                )
                if status >= 400:
                    problems.append(
                        f"{app.hostname}: signed in, but the app answered {status}"
                    )
        finally:
            context.close()
    assert not problems, "\n".join(problems)


@pytest.fixture(scope="module")
def hub(cluster) -> apps.NebariApp:
    cluster.require_app(DATA_SCIENCE_PACK_APP)
    namespace = apps.destination_namespace(cluster, DATA_SCIENCE_PACK_APP)
    found = [
        a
        for a in apps.nebari_apps_in(cluster, namespace or "")
        if a.auth_enabled and not a.enforce_at_gateway
    ]
    assert len(found) == 1, (
        f"expected one JupyterHub NebariApp in {namespace!r}, found "
        f"{[a.ref for a in found]}"
    )
    return apps.wait_for_app_ready(cluster, found[0])


@pytest.mark.ui
def test_a_new_user_can_sign_in_to_jupyterhub(page, hub, keycloak_issuer, scratch_user):
    """JupyterHub runs its own Keycloak login rather than the gateway's, so
    it is a separate path through the same realm."""
    login_via_keycloak(
        page,
        f"https://{hub.hostname}/hub/",
        scratch_user["username"],
        scratch_user["password"],
    )
    assert page_host(page.url) != apps.host_of(keycloak_issuer), (
        f"still on Keycloak after signing in to JupyterHub: {page.url}"
    )
    assert page_host(page.url) == hub.hostname, f"ended up on {page.url}"
    assert "/hub/login" not in page.url, (
        f"JupyterHub sent the user back to login: {page.url}"
    )


@pytest.mark.ui
def test_jupyterhub_trades_a_users_login_for_nebi_access(
    cluster, page, hub, keycloak_issuer, scratch_user
):
    """Signed in to JupyterHub, a user's Nebi environments are offered in
    jhub-apps. Behind that is a token exchange: the hub refreshes the
    user's Keycloak token, exchanges it for one with Nebi's audience, and
    trades that for a Nebi session.

    A brand-new user owns no environments, so an empty list is the right
    answer and proves nothing on its own. The hub's log is what shows the
    exchange reached Nebi for this user.
    """
    cluster.require_app(NEBI_PACK_APP)
    started = datetime.now(UTC)

    login_via_keycloak(
        page,
        f"https://{hub.hostname}/hub/",
        scratch_user["username"],
        scratch_user["password"],
    )
    assert page_host(page.url) == hub.hostname, f"sign-in did not complete: {page.url}"

    follow_login_redirects(page, f"https://{hub.hostname}{JHUB_APPS_LOGIN_PATH}")
    approve_hub_oauth_consent(page, JHUB_APPS_SESSION_COOKIE)
    assert wait_for_cookie(page.context, JHUB_APPS_SESSION_COOKIE), (
        f"jhub-apps never set its {JHUB_APPS_SESSION_COOKIE} session cookie "
        f"after its OAuth round trip with the hub; the browser is at {page.url}"
    )

    response = page.goto(f"https://{hub.hostname}{JHUB_APPS_ENVIRONMENTS_PATH}")
    assert response is not None and response.ok, (
        f"jhub-apps environment list answered "
        f"{response.status if response else 'nothing'} at {page.url}"
    )

    logs = apps.pod_logs_since(cluster, hub.namespace, HUB_POD_SELECTOR, started)
    user = scratch_user["username"]
    mine = [
        line for line in logs.splitlines() if user in line or "token-exchange" in line
    ]
    failures = [line for line in mine if any(f in line for f in EXCHANGE_FAILURES)]
    listed = [line for line in mine if ENVIRONMENTS_LISTED in line and user in line]
    assert not failures, "token exchange failed:\n" + "\n".join(failures)
    assert listed, (
        f"the hub never listed Nebi environments for {user}; its log since the "
        "journey started:\n" + "\n".join(logs.splitlines()[-40:])
    )
