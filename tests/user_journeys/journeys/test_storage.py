"""Storage journeys.

The platform's storage promise is that a user's data outlives the pod
that wrote it. These journeys exercise that promise rather than
asserting that Longhorn looks healthy.
"""

import pytest

from nebari_journeys import constants

VOLUME_SIZE = "1Gi"
CANARY_PATH = "/data/journey-canary"
CANARY_TEXT = "nebari-journey-canary"


@pytest.mark.slow
def test_a_users_data_survives_their_pod(cluster, scratch_namespace):
    """Data written by one pod must be readable by a different pod that
    mounts the same volume after the first pod is fully gone."""
    ns = scratch_namespace
    storage_class = cluster.default_storage_class()

    ns.request_volume("data", VOLUME_SIZE, storage_class)
    ns.wait_volume_bound("data")

    ns.run_pod("writer", pvc_name="data")
    ns.wait_pod_ready("writer")
    ns.exec("writer", ["sh", "-c", f"echo {CANARY_TEXT} > {CANARY_PATH}"])
    # ReadWriteOnce means the volume can only be mounted by one pod at a
    # time. delete_pod() waits for the writer to be fully gone, not just
    # for the delete call to return, so the reader below never races the
    # writer for the mount and a flaky "volume still attached" failure
    # cannot masquerade as a storage bug.
    ns.delete_pod("writer")

    ns.run_pod("reader", pvc_name="data")
    ns.wait_pod_ready("reader")
    recovered = ns.exec("reader", ["sh", "-c", f"cat {CANARY_PATH}"])

    assert CANARY_TEXT in recovered, (
        "data written before the pod was replaced did not survive; "
        f"expected to find {CANARY_TEXT!r} in {CANARY_PATH}, "
        f"but the replacement pod read back {recovered!r}"
    )


@pytest.mark.slow
def test_a_users_volume_is_actually_replicated(cluster, scratch_namespace):
    """Bound is not the same as durable. A volume with one healthy replica
    survives a pod restart but not a node loss."""
    ns = scratch_namespace
    ns.request_volume("replicated", VOLUME_SIZE, cluster.default_storage_class())
    ns.wait_volume_bound("replicated")

    pvc = cluster.core.read_namespaced_persistent_volume_claim(
        name="replicated", namespace=ns.name
    )
    volume = cluster.longhorn_volume(pvc.spec.volume_name)

    expected = volume["spec"]["numberOfReplicas"]
    healthy = volume.get("status", {}).get("robustness")
    assert expected >= 1, (
        f"Longhorn volume {pvc.spec.volume_name!r} is configured with "
        f"{expected} replicas; expected at least 1"
    )
    assert healthy == "healthy", (
        f"Longhorn volume {pvc.spec.volume_name!r} has {expected} configured "
        f"replicas but reports robustness {healthy!r}, not 'healthy'; a "
        "degraded or faulted volume does not survive a node loss"
    )


@pytest.mark.slow
def test_backups_are_configured_when_the_platform_ships_them(cluster):
    """Skipped on clusters without longhorn-backup, per the optional-component
    rule: an absent component is not a failing journey."""
    cluster.require_app("longhorn-backup")

    targets = cluster.custom.list_namespaced_custom_object(
        group="longhorn.io",
        version="v1beta2",
        namespace=constants.LONGHORN_NAMESPACE,
        plural="backuptargets",
    )["items"]
    assert targets, (
        "longhorn-backup ArgoCD application is deployed but no BackupTarget "
        f"custom object exists in namespace {constants.LONGHORN_NAMESPACE!r}"
    )

    available = [t for t in targets if t.get("status", {}).get("available") is True]
    assert available, (
        "no Longhorn BackupTarget reports status.available == True "
        f"(found: {[t.get('status', {}).get('available') for t in targets]}); "
        "backup credentials or endpoint are likely misconfigured"
    )

    jobs = cluster.custom.list_namespaced_custom_object(
        group="longhorn.io",
        version="v1beta2",
        namespace=constants.LONGHORN_NAMESPACE,
        plural="recurringjobs",
    )["items"]
    assert jobs, (
        "longhorn-backup ArgoCD application is deployed but no RecurringJob "
        f"custom object exists in namespace {constants.LONGHORN_NAMESPACE!r}; "
        "backups will never run on a schedule"
    )
