from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest

from nebari_journeys import disruption


def pod(uid, ready=True, deleting=False):
    return SimpleNamespace(
        metadata=SimpleNamespace(
            uid=uid, name=f"pod-{uid}", deletion_timestamp="t" if deleting else None
        ),
        status=SimpleNamespace(
            conditions=[
                SimpleNamespace(type="Ready", status="True" if ready else "False")
            ]
        ),
    )


def cluster_listing(*rounds):
    cluster = MagicMock()
    cluster.core.list_namespaced_pod.side_effect = [
        SimpleNamespace(items=r) for r in rounds
    ]
    return cluster


def test_restart_deletes_every_matching_pod_and_waits_for_ready_replacements(
    monkeypatch,
):
    monkeypatch.setattr(disruption.time, "sleep", lambda s: None)
    cluster = cluster_listing(
        [pod("a")],
        [pod("a", deleting=True), pod("b", ready=False)],
        [pod("b")],
    )
    assert disruption.restart_pods(cluster, "ns", "app=x") == ["pod-b"]
    cluster.core.delete_namespaced_pod.assert_called_once_with("pod-a", "ns")


def test_restart_refuses_a_selector_that_matches_nothing():
    cluster = cluster_listing([])
    with pytest.raises(RuntimeError, match="no pods match"):
        disruption.restart_pods(cluster, "ns", "app=x")


def test_restart_times_out_when_replacements_never_become_ready(monkeypatch):
    clock = iter([0.0, 0.0, disruption.RESTART_TIMEOUT + 1])
    monkeypatch.setattr(disruption.time, "monotonic", lambda: next(clock))
    monkeypatch.setattr(disruption.time, "sleep", lambda s: None)
    cluster = cluster_listing([pod("a")], [pod("b", ready=False)])
    with pytest.raises(TimeoutError):
        disruption.restart_pods(cluster, "ns", "app=x")
