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
def keycloak(cluster, platform_domain, trust_anchor, dns_mapping):
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
        except requests.exceptions.RequestException as exc:
            print(f"failed to delete scratch user {username}: {exc}")
