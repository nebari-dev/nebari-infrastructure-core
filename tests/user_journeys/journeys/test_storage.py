"""Storage journeys.

The platform's storage promise is that a user's data outlives the pod
that wrote it. These journeys exercise that promise rather than
asserting that Longhorn looks healthy.

All three require Longhorn and skip without it. Longhorn is the platform's
own storage layer, and it is optional: a Nebari cluster on EKS or GKE can run
entirely on the cloud provider's storage. `default_storage_class()` is
deliberately NOT used here. On EKS it returns `gp2`/`gp3` and on GKE
`standard`, so these journeys would silently test the cloud provider's
provisioner while claiming to test Longhorn's promise, and the replication
journey would then raise a bare 404 looking for a Longhorn Volume behind an
EBS-backed PV.
"""

import pytest

from nebari_journeys import constants
from nebari_journeys.cluster import LONGHORN_STORAGE_CLASS

VOLUME_SIZE = "1Gi"
CANARY_PATH = "/data/journey-canary"
CANARY_TEXT = "nebari-journey-canary"

# A pod that mounts a Longhorn volume waits on more than a scheduler
# decision and an image pull: Longhorn also has to attach the volume,
# which means starting or reusing an instance-manager pod and running the
# CSI attacher round trip. A first run on EKS timed out at the default
# 180s waiting for exactly that. Pods that mount nothing keep the default.
POD_MOUNTS_LONGHORN_TIMEOUT = 300


def _replication_unverified_message(
    schedulable_nodes: int, expected_replicas: int
) -> str:
    """Names both numbers and states the risk, not just the mechanics.

    Split out so tests_lib can pin the exact wording without needing a
    live cluster, since it is the whole point of the message: a reader
    who only sees "1 < 2" has no idea that means their data will not
    survive a node loss.
    """
    return (
        f"{schedulable_nodes} schedulable Longhorn node(s) cannot satisfy "
        f"numberOfReplicas={expected_replicas}, so every volume on this cluster "
        "is degraded and will not survive node loss. Replication is "
        "UNVERIFIED here."
    )


@pytest.mark.slow
def test_a_users_data_survives_their_pod(cluster, scratch_namespace):
    """Data written by one pod must be readable by a different pod that
    mounts the same volume after the first pod is fully gone."""
    cluster.require_longhorn()
    ns = scratch_namespace

    ns.request_volume("data", VOLUME_SIZE, LONGHORN_STORAGE_CLASS)
    ns.wait_volume_bound("data")

    ns.run_pod("writer", pvc_name="data")
    ns.wait_pod_ready("writer", timeout=POD_MOUNTS_LONGHORN_TIMEOUT)
    ns.exec("writer", ["sh", "-c", f"echo {CANARY_TEXT} > {CANARY_PATH}"])
    # ReadWriteOnce means the volume can only be mounted by one pod at a
    # time. delete_pod() waits for the writer to be fully gone, not just
    # for the delete call to return, so the reader below never races the
    # writer for the mount and a flaky "volume still attached" failure
    # cannot masquerade as a storage bug.
    ns.delete_pod("writer")

    ns.run_pod("reader", pvc_name="data")
    ns.wait_pod_ready("reader", timeout=POD_MOUNTS_LONGHORN_TIMEOUT)
    recovered = ns.exec("reader", ["sh", "-c", f"cat {CANARY_PATH}"])

    assert CANARY_TEXT in recovered, (
        "data written before the pod was replaced did not survive; "
        f"expected to find {CANARY_TEXT!r} in {CANARY_PATH}, "
        f"but the replacement pod read back {recovered!r}"
    )


@pytest.mark.slow
def test_a_users_volume_is_actually_replicated(cluster, scratch_namespace):
    """Bound is not the same as durable. A volume with one healthy replica
    survives a pod restart but not a node loss.

    The volume is ATTACHED to a pod before its robustness is read, and
    robustness is then POLLED until it settles rather than sampled once. Longhorn
    only computes robustness for an attached volume: a provisioned but
    detached volume sits at state `detached`, robustness `unknown`, because
    its replicas are not running and there is nothing to judge. The PVC binds
    regardless (the CSI provisioner creates the Volume CR at provision time),
    so reading robustness without attaching reports `unknown` on a perfectly
    healthy, correctly replicated cluster.
    """
    cluster.require_longhorn()
    ns = scratch_namespace
    ns.request_volume("replicated", VOLUME_SIZE, LONGHORN_STORAGE_CLASS)
    ns.wait_volume_bound("replicated")

    ns.run_pod("attacher", pvc_name="replicated")
    ns.wait_pod_ready("attacher", timeout=POD_MOUNTS_LONGHORN_TIMEOUT)

    pvc = cluster.core.read_namespaced_persistent_volume_claim(
        name="replicated", namespace=ns.name
    )
    # Read once first, only to decide whether this cluster can replicate at
    # all. The skip below is a physical fact about the topology, not
    # something that converges, so waiting for robustness before deciding it
    # would spend the whole settle timeout to reach a foregone skip -- on
    # single-node clusters, which is the common case for the skip.
    volume = cluster.longhorn_volume(pvc.spec.volume_name)
    expected = volume["spec"]["numberOfReplicas"]

    schedulable_nodes = cluster.schedulable_longhorn_node_count()
    if schedulable_nodes < expected:
        pytest.skip(_replication_unverified_message(schedulable_nodes, expected))

    # This cluster CAN satisfy the replica count, so robustness is now worth
    # waiting on: a just-attached volume reports `degraded` while Longhorn
    # rebuilds its replicas, and reading it the instant the pod goes Ready
    # calls a healthy cluster degraded. See Cluster.settled_longhorn_volume.
    volume = cluster.settled_longhorn_volume(pvc.spec.volume_name)
    state = volume.get("status", {}).get("state")
    healthy = volume.get("status", {}).get("robustness")

    assert expected >= 1, (
        f"Longhorn volume {pvc.spec.volume_name!r} is configured with "
        f"{expected} replicas; expected at least 1"
    )
    assert state == "attached", (
        f"Longhorn volume {pvc.spec.volume_name!r} reports state {state!r} while a "
        "ready pod has it mounted; robustness is only meaningful for an attached "
        "volume, so this is a Longhorn attachment problem rather than a "
        "replication result"
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
    cluster.require_longhorn()
    cluster.require_app(constants.LONGHORN_BACKUP_APP)

    targets = cluster.custom.list_namespaced_custom_object(
        group="longhorn.io",
        version="v1beta2",
        namespace=constants.LONGHORN_NAMESPACE,
        plural="backuptargets",
    )["items"]
    assert targets, (
        f"{constants.LONGHORN_BACKUP_APP} ArgoCD application is deployed but no "
        "BackupTarget custom object exists in namespace "
        f"{constants.LONGHORN_NAMESPACE!r}"
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
        f"{constants.LONGHORN_BACKUP_APP} ArgoCD application is deployed but no "
        "RecurringJob custom object exists in namespace "
        f"{constants.LONGHORN_NAMESPACE!r}; backups will never run on a schedule"
    )
