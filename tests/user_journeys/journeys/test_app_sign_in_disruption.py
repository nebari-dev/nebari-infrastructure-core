"""Sign-in survives the platform components behind it being replaced.

These are the only journeys that act outside a scratch namespace: each one
restarts a platform pod, which briefly interrupts the cluster for every
user. They are marked `disruptive` and run only with --allow-disruption.

Each journey signs a user in first, replaces one component, and then checks
what a user would notice: the session they already had still works, the
gateway still sends a newcomer to Keycloak, and (where the component takes
part in sign-in) a fresh sign-in still completes. The operator journey also
checks that a restart did not make the operator rewrite policies it had
already reconciled, which is what a flapping reconcile would look like.
"""

import time

import pytest

from nebari_journeys import apps, disruption
from nebari_journeys.ui import login_via_keycloak, page_host

pytestmark = [pytest.mark.disruptive, pytest.mark.slow, pytest.mark.ui]

# After a restart, how long to let the operator reconcile before judging
# whether it rewrote anything.
RECONCILE_SETTLE_SECONDS = 30


@pytest.fixture(scope="module")
def app(cluster) -> apps.NebariApp:
    found = [a for a in apps.nebari_apps(cluster) if a.enforce_at_gateway]
    if not found:
        pytest.skip("no NebariApp on this cluster leaves sign-in to the gateway")
    return apps.wait_for_app_ready(cluster, found[0])


@pytest.fixture(scope="module")
def keycloak_host(platform_domain, trust_anchor, dns_mapping, gateway_reachable):
    return apps.host_of(apps.advertised_issuer(platform_domain, trust_anchor or True))


def sign_in(browser, browser_context_args, app, user) -> dict[str, str]:
    context = browser.new_context(**browser_context_args)
    try:
        page = context.new_page()
        login_via_keycloak(
            page, f"https://{app.hostname}/", user["username"], user["password"]
        )
        assert page_host(page.url) == app.hostname, (
            f"sign-in did not complete: {page.url}"
        )
        return apps.cookies_for_host(context, app.hostname)
    finally:
        context.close()


def assert_sign_in_still_works(app, cookies, keycloak_host, verify, cluster):
    existing = apps.visit_with_cookies(f"https://{app.hostname}/", cookies, verify)
    assert existing.status_code < 300, (
        f"the session from before the restart is no longer served: HTTP {existing.status_code}"
    )
    anonymous = apps.anonymous_visit(f"https://{app.hostname}/", verify)
    assert (
        anonymous.is_redirect
        and apps.host_of(anonymous.headers.get("Location", "")) == keycloak_host
    ), f"after the restart, an anonymous visitor got HTTP {anonymous.status_code}"
    accepted = apps.acceptance(apps.security_policies_for(cluster, app)[0])
    assert accepted and all(c.get("status") == "True" for c in accepted), (
        f"{app.ref}'s SecurityPolicy is no longer accepted: {accepted}"
    )


def test_sign_in_survives_an_operator_restart(
    cluster,
    browser,
    browser_context_args,
    app,
    keycloak_host,
    scratch_user,
    trust_anchor,
):
    cookies = sign_in(browser, browser_context_args, app, scratch_user)
    before = apps.security_policies_for(cluster, app)[0]["metadata"]

    disruption.restart_pods(
        cluster, disruption.OPERATOR_NAMESPACE, disruption.OPERATOR_SELECTOR
    )
    time.sleep(RECONCILE_SETTLE_SECONDS)

    after = apps.security_policies_for(cluster, app)
    assert len(after) == 1, f"{len(after)} SecurityPolicies after the restart"
    assert after[0]["metadata"]["uid"] == before["uid"], (
        "the operator deleted and recreated the SecurityPolicy on restart"
    )
    assert after[0]["metadata"]["generation"] == before["generation"], (
        "the operator rewrote an already-reconciled SecurityPolicy on restart "
        f"(generation {before['generation']} -> {after[0]['metadata']['generation']})"
    )
    assert_sign_in_still_works(
        app, cookies, keycloak_host, trust_anchor or True, cluster
    )


def test_sign_in_survives_a_keycloak_restart(
    cluster,
    browser,
    browser_context_args,
    app,
    keycloak_host,
    scratch_user,
    trust_anchor,
):
    """The gateway validates an existing session on its own, and a new
    sign-in goes through the restarted Keycloak, including the token
    endpoint the policy pins to Keycloak's in-cluster address."""
    cookies = sign_in(browser, browser_context_args, app, scratch_user)

    disruption.restart_pods(
        cluster, disruption.KEYCLOAK_NAMESPACE, disruption.KEYCLOAK_SELECTOR
    )

    assert_sign_in_still_works(
        app, cookies, keycloak_host, trust_anchor or True, cluster
    )
    sign_in(browser, browser_context_args, app, scratch_user)


def test_sign_in_survives_an_envoy_proxy_restart(
    cluster,
    browser,
    browser_context_args,
    app,
    keycloak_host,
    scratch_user,
    trust_anchor,
):
    """Session cookies are signed with a key Envoy Gateway keeps outside the
    proxy, so a new proxy must still accept the sessions the old one
    issued."""
    cookies = sign_in(browser, browser_context_args, app, scratch_user)

    disruption.restart_pods(
        cluster, disruption.ENVOY_NAMESPACE, disruption.ENVOY_PROXY_SELECTOR
    )

    assert_sign_in_still_works(
        app, cookies, keycloak_host, trust_anchor or True, cluster
    )
    sign_in(browser, browser_context_args, app, scratch_user)
