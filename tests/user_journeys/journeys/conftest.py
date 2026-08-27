"""Fixtures shared by every journey.

Journeys need a cluster. The library's own tests live in tests_lib/,
outside this directory, so nothing here runs for them: an autouse
fixture only applies beneath the conftest that defines it.
"""

import pytest

from nebari_journeys import trust
from nebari_journeys.cluster import Cluster
from nebari_journeys.k8s import (
    SCRATCH_NAMESPACE_PREFIX,
    ScratchNamespace,
    scratch_namespace_name,
    sweep_stale_namespaces,
)


@pytest.fixture(scope="session")
def cluster() -> Cluster:
    return Cluster.connect()


@pytest.fixture(scope="session", autouse=True)
def sweep_leftovers(cluster):
    """Remove journey namespaces left behind by earlier crashed runs."""
    result = sweep_stale_namespaces(cluster)
    if result.deleted:
        print(f"swept stale journey namespaces: {', '.join(result.deleted)}")
    if result.skipped:
        # A labeled namespace whose name does not match the scratch prefix
        # is an anomaly, not a routine outcome: it was left undeleted on
        # purpose, and someone needs to see it.
        print(
            "!!! ANOMALY: namespaces carry the journey label but do not "
            f"match the scratch prefix, and were NOT deleted: {', '.join(result.skipped)}"
        )
    return result


@pytest.fixture(scope="session")
def gateway_address(cluster) -> str:
    return cluster.gateway_address()


@pytest.fixture(scope="session")
def platform_domain(cluster) -> str:
    return cluster.domain()


@pytest.fixture(scope="session")
def gateway_reachable(platform_domain, gateway_address) -> bool:
    """Probe the gateway ONCE per session and fail every journey that
    needs it with a single precise diagnosis, instead of letting each one
    independently burn a 30 second connect timeout discovering the same
    unreachable address.

    Deliberately NOT autouse: test_smoke.py and the storage journeys talk
    to the Kubernetes API only and must keep running when the gateway is
    unroutable. Only fixtures that actually need gateway HTTP access
    (keycloak, and anything downstream of it) depend on this.

    Fails rather than skips: an unreachable gateway means a real user
    cannot reach Keycloak either, so this is a platform failure, not an
    environment quirk to shrug off.
    """
    port = 443
    if not trust.gateway_reachable(gateway_address, port=port):
        pytest.fail(
            f"gateway for platform domain {platform_domain!r} is not reachable: "
            f"TCP connect to {gateway_address}:{port} failed. Every journey that "
            "needs Keycloak, ArgoCD, or the Longhorn UI cannot proceed, since a "
            "real user could not reach them either. A gateway address that is "
            "not routable from this host is a common cause -- for example a "
            "MetalLB address pool that does not overlap the kind cluster's "
            "Docker network (see issue #612) -- so confirm the gateway address "
            "is actually reachable from this host before re-running."
        )
    return True


@pytest.fixture(scope="session")
def trust_anchor(cluster, tmp_path_factory) -> str | None:
    """Path to a CA file, or None when the system trust store suffices."""
    pem = trust.trust_anchor_pem(cluster)
    return trust.write_trust_anchor(pem, tmp_path_factory.mktemp("trust"))


@pytest.fixture(scope="session", autouse=True)
def dns_mapping(platform_domain, gateway_address):
    """Map the platform domain to the gateway only when DNS does not already."""
    if not trust.needs_dns_mapping(platform_domain, gateway_address):
        yield False
        return
    undo = trust.install_dns_mapping(platform_domain, gateway_address)
    try:
        yield True
    finally:
        undo()


@pytest.fixture
def scratch_namespace(cluster, request):
    ns = ScratchNamespace(cluster, scratch_namespace_name())
    ns.create()
    try:
        yield ns
    finally:
        if request.config.getoption("--keep-namespace"):
            print(f"--keep-namespace set; leaving {ns.name} in place")
        else:
            ns.delete()


@pytest.fixture(scope="session")
def keycloak(cluster, platform_domain, trust_anchor, dns_mapping, gateway_reachable):
    from nebari_journeys.keycloak import Keycloak

    return Keycloak.for_cluster(cluster, platform_domain, trust_anchor)


@pytest.fixture
def scratch_user(keycloak):
    """A throwaway realm user, deleted afterwards even when the test fails."""
    import requests

    from nebari_journeys.keycloak import generated_password

    # Built from the same SCRATCH_NAMESPACE_PREFIX that scratch_namespace_name()
    # uses, rather than a hardcoded literal, so the two cannot drift apart.
    username = "journey-" + scratch_namespace_name().removeprefix(
        SCRATCH_NAMESPACE_PREFIX
    )
    password = generated_password()
    user_id = keycloak.create_user(username, password)
    try:
        yield {"id": user_id, "username": username, "password": password}
    finally:
        try:
            keycloak.delete_user(user_id)
        except (requests.exceptions.RequestException, KeyError, ValueError) as exc:
            print(f"failed to delete scratch user {username}: {exc}")


@pytest.fixture(scope="session")
def browser_type_launch_args(
    browser_type_launch_args, platform_domain, gateway_address, dns_mapping
):
    """Map the platform domain for Chromium when public DNS does not."""
    args = list(browser_type_launch_args.get("args", []))
    args.extend(trust.chromium_args(platform_domain, gateway_address, dns_mapping))
    return {**browser_type_launch_args, "args": args}


@pytest.fixture(scope="session")
def browser_context_args(browser_context_args, trust_anchor):
    """Left unchanged: `ignore_https_errors` is forbidden in this suite, and
    `client_certificates` is for TLS *client* authentication (mTLS), not for
    trusting a server's CA, so it does nothing for a self-signed gateway.

    Chromium trusts a custom CA through the OS/NSS trust store, not through
    any BrowserContext option. When `trust_anchor` is not None, the anchor
    still needs to land in the trust store the browser reads at launch;
    that installation is Task 11's job (a CI setup step), not something
    expressible here. Do not reach for `ignore_https_errors` to work around
    a rejected self-signed chain: fix the trust store instead.
    """
    return dict(browser_context_args)
