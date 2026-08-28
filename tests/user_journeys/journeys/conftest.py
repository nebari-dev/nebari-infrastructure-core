"""Fixtures shared by every journey.

Journeys need a cluster. The library's own tests live in tests_lib/,
outside this directory, so nothing here runs for them: an autouse
fixture only applies beneath the conftest that defines it.
"""

import warnings

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
def trust_anchor_pem(cluster) -> str | None:
    """The raw anchor PEM, or None when the system trust store suffices.

    Fetched once per session and shared by `trust_anchor` (the file path
    `requests` and Playwright's context options read) and by Chromium's
    launch args (which need the PEM itself to compute the SPKI pin), so
    the cluster's secret is read only once.
    """
    return trust.trust_anchor_pem(cluster)


@pytest.fixture(scope="session")
def trust_anchor(trust_anchor_pem, tmp_path_factory) -> str | None:
    """Path to a CA file, or None when the system trust store suffices."""
    return trust.write_trust_anchor(trust_anchor_pem, tmp_path_factory.mktemp("trust"))


SELF_SIGNED_WARNING = (
    "self signed cert detected, skipping tls tests "
    "(see nebari_journeys.trust.is_self_signed_leaf and issue #447 for the "
    "real fix: cert-manager issuing a proper CA for the gateway)"
)

SELF_SIGNED_TRUSTED_CA_WARNING = (
    "self signed cert detected: ArgoCD cannot trust the gateway certificate for "
    "server-side OIDC discovery, so ArgoCD SSO is UNVERIFIED and known broken on "
    "this cluster (#490, root cause #447; #607 blocks in-cluster resolution of the "
    "issuer URL on the same shape). A cluster with a real issuing CA runs this "
    "journey normally."
)


@pytest.fixture(scope="session")
def self_signed_anchor(trust_anchor_pem) -> bool:
    """True when the derived anchor is a self-signed leaf, not a real CA.

    This is the shape cert-manager's default `selfsigned-issuer` produces
    (see the nebari_journeys.trust module docstring): a `CA:FALSE`
    end-entity certificate that Chromium cannot accept as a trust anchor
    by any NSS trick. Browser journeys still run, pinned to that
    certificate's public key via chromium_args' SPKI flag; only journeys
    whose SUBJECT is certificate validity or chain trust itself (marked
    `tls`) are skipped, since those cannot pass against a leaf that was
    never meant to be an anchor.
    """
    if trust_anchor_pem is None:
        return False
    return trust.is_self_signed_leaf(trust_anchor_pem)


@pytest.fixture(scope="session", autouse=True)
def warn_once_if_self_signed(self_signed_anchor):
    """Surface the relaxation once per session, even if no `tls`-marked
    journey exists yet to be skipped by it, so an operator running
    locally can see that certificate validation has been narrowed for
    the browser rather than discovering it later. A UserWarning, the same
    mechanism test_smoke.py uses for drift reporting, surfaces in
    pytest's warnings summary without needing -s, and does not fail
    the run.
    """
    if self_signed_anchor:
        warnings.warn(SELF_SIGNED_WARNING, UserWarning, stacklevel=2)


@pytest.fixture(autouse=True)
def skip_tls_marked_tests_on_self_signed(request, self_signed_anchor):
    """Skip `tls`-marked journeys when the anchor is a self-signed leaf.

    Scoped narrowly to tests whose SUBJECT is certificate validity or
    chain trust itself: everything else (including every browser journey
    that merely travels over TLS) keeps running, pinned by the SPKI flag
    in chromium_args.
    """
    if self_signed_anchor and request.node.get_closest_marker("tls"):
        pytest.skip(SELF_SIGNED_WARNING)


@pytest.fixture(autouse=True)
def skip_trusted_ca_marked_tests_on_self_signed(request, self_signed_anchor):
    """Skip `requires_trusted_ca`-marked journeys when the anchor is a
    self-signed leaf.

    This is a different failure shape from `tls`-marked journeys: those
    fail because the TEST RUNNER cannot validate the chain. Here, the
    runner's own trust is irrelevant -- it is a THIRD PARTY (ArgoCD's
    server, performing OIDC discovery against the external Keycloak URL)
    that cannot trust the gateway's self-signed leaf. No SPKI pin fixes
    that: the pin only relaxes Chromium's trust in this process, and
    ArgoCD's server is a separate process with no such pin. See issues
    #490 (ArgoCD's OIDC discovery fails TLS verification), #447 (root
    cause: cert-manager's selfsigned-issuer produces a CA:FALSE leaf,
    not a proper CA), and #607 (a second, separate blocker on the same
    cluster shape: the external hostname does not resolve in-cluster at
    all).
    """
    if self_signed_anchor and request.node.get_closest_marker("requires_trusted_ca"):
        pytest.skip(SELF_SIGNED_TRUSTED_CA_WARNING)


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
    browser_type_launch_args,
    platform_domain,
    gateway_address,
    dns_mapping,
    trust_anchor_pem,
):
    """Map the platform domain for Chromium, and when a trust anchor was
    derived from the cluster (a self-signed leaf, not a proper CA -- see
    nebari_journeys.trust module docstring), pin Chromium's trust to its
    exact public key via --ignore-certificate-errors-spki-list. When no
    anchor was derived, the chain is publicly trusted (ACME) and nothing
    extra is added: Chromium runs exactly as a real user's browser would.
    """
    args = list(browser_type_launch_args.get("args", []))
    args.extend(
        trust.chromium_args(
            platform_domain, gateway_address, dns_mapping, trust_anchor_pem
        )
    )
    return {**browser_type_launch_args, "args": args}


@pytest.fixture(scope="session")
def browser_context_args(browser_context_args, trust_anchor):
    """Left unchanged: `ignore_https_errors` is forbidden in this suite, and
    `client_certificates` is for TLS *client* authentication (mTLS), not for
    trusting a server's CA, so it does nothing for a self-signed gateway.

    Trust for a self-signed gateway certificate is established at Chromium
    launch, via --ignore-certificate-errors-spki-list in
    `browser_type_launch_args` (see that fixture and the nebari_journeys.trust
    module docstring for the trade this makes), not through any
    BrowserContext option. Do not reach for `ignore_https_errors` to work
    around a rejected self-signed chain: the SPKI pin is the fix.
    """
    return dict(browser_context_args)
