"""Unit tests for the storage journeys' replication-check branching.

journeys/test_storage.py needs a live cluster to collect through pytest
normally, but test_a_users_volume_is_actually_replicated is a plain
function once `cluster` and `scratch_namespace` are given mocks, so it
is exercised directly here for all three branches: skip when this
cluster physically cannot replicate, pass when it can and is healthy,
fail when it can and is not. Imported under a name that does not start
with `test_` so pytest does not also try to collect it here and fail
looking for real `cluster`/`scratch_namespace` fixtures.
"""

from unittest.mock import MagicMock

import pytest

from journeys.test_storage import _replication_unverified_message
from journeys.test_storage import (
    test_a_users_volume_is_actually_replicated as replication_check,
)


def _replication_cluster(schedulable_nodes, expected_replicas, state, robustness):
    cluster = MagicMock()
    cluster.schedulable_longhorn_node_count.return_value = schedulable_nodes
    pvc = MagicMock()
    pvc.spec.volume_name = "pvc-replicated"
    cluster.core.read_namespaced_persistent_volume_claim.return_value = pvc
    volume = {
        "spec": {"numberOfReplicas": expected_replicas},
        "status": {"state": state, "robustness": robustness},
    }
    cluster.longhorn_volume.return_value = volume
    # The journey must read the SETTLED volume: robustness is `degraded`
    # while Longhorn rebuilds replicas after an attach, so a single read
    # calls a healthy cluster degraded.
    cluster.settled_longhorn_volume.return_value = volume
    return cluster


def _namespace():
    ns = MagicMock()
    ns.name = "nebari-journey-test"
    return ns


def test_replication_unverified_message_names_both_numbers_and_the_risk():
    message = _replication_unverified_message(1, 2)
    assert "1" in message
    assert "numberOfReplicas=2" in message
    assert "UNVERIFIED" in message
    assert "node loss" in message


def test_skips_when_nodes_cannot_satisfy_the_configured_replica_count():
    """1 schedulable node, 2 configured replicas: a physical limit, so this
    is a skip naming both numbers, not a permanent red."""
    cluster = _replication_cluster(
        schedulable_nodes=1,
        expected_replicas=2,
        state="attached",
        robustness="degraded",
    )
    with pytest.raises(pytest.skip.Exception, match="numberOfReplicas=2"):
        replication_check(cluster, _namespace())


def test_passes_when_nodes_satisfy_replicas_and_the_volume_is_healthy():
    """Enough nodes to replicate, and Longhorn reports healthy: the
    strict assertion path is unaffected by the new skip branch."""
    cluster = _replication_cluster(
        schedulable_nodes=2,
        expected_replicas=2,
        state="attached",
        robustness="healthy",
    )
    assert replication_check(cluster, _namespace()) is None


def test_fails_when_nodes_satisfy_replicas_but_the_volume_is_degraded():
    """Enough nodes to replicate, but the volume is degraded anyway: a
    cluster that COULD replicate must still fail, never skip."""
    cluster = _replication_cluster(
        schedulable_nodes=2,
        expected_replicas=2,
        state="attached",
        robustness="degraded",
    )
    with pytest.raises(AssertionError, match="degraded"):
        replication_check(cluster, _namespace())


def test_robustness_is_read_from_the_settled_volume_not_a_single_sample():
    """Pins the fix for the read-immediately-after-attach flake: robustness
    must come from the settled read, or it is sampled while Longhorn is
    still rebuilding replicas."""
    cluster = _replication_cluster(
        schedulable_nodes=3,
        expected_replicas=2,
        state="attached",
        robustness="healthy",
    )
    replication_check(cluster, _namespace())
    cluster.settled_longhorn_volume.assert_called_once_with("pvc-replicated")


def test_the_skip_decision_does_not_wait_for_robustness_to_settle():
    """On a single-node cluster robustness can never become healthy, so
    polling before deciding to skip would spend the whole settle timeout to
    reach a foregone conclusion -- and single-node is the common case for
    this skip."""
    cluster = _replication_cluster(
        schedulable_nodes=1,
        expected_replicas=2,
        state="attached",
        robustness="degraded",
    )
    with pytest.raises(pytest.skip.Exception):
        replication_check(cluster, _namespace())
    cluster.settled_longhorn_volume.assert_not_called()
