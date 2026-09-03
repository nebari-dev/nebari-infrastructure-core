import re
from unittest.mock import MagicMock

import pytest
from kubernetes.client.rest import ApiException

from nebari_journeys import constants
from nebari_journeys.k8s import (
    ScratchNamespace,
    scratch_namespace_name,
    sweep_stale_namespaces,
)


def _ns(cluster=None):
    return ScratchNamespace(cluster or MagicMock(), "nebari-journey-abcd1234")


def test_scratch_namespace_name_has_the_expected_shape():
    name = scratch_namespace_name()
    assert re.fullmatch(r"nebari-journey-[0-9a-f]{8}", name)


def test_scratch_namespace_names_are_unique():
    assert len({scratch_namespace_name() for _ in range(50)}) == 50


def test_create_applies_the_journey_label():
    cluster = MagicMock()
    _ns(cluster).create()
    body = cluster.core.create_namespace.call_args.kwargs["body"]
    labels = body["metadata"]["labels"]
    assert labels[constants.JOURNEY_LABEL_KEY] == constants.JOURNEY_LABEL_VALUE


def test_create_uses_the_generated_name():
    cluster = MagicMock()
    _ns(cluster).create()
    body = cluster.core.create_namespace.call_args.kwargs["body"]
    assert body["metadata"]["name"] == "nebari-journey-abcd1234"


def test_request_volume_sets_size_and_storage_class():
    cluster = MagicMock()
    _ns(cluster).request_volume("data", "5Gi", "longhorn")
    body = cluster.core.create_namespaced_persistent_volume_claim.call_args.kwargs[
        "body"
    ]
    assert body["spec"]["resources"]["requests"]["storage"] == "5Gi"
    assert body["spec"]["storageClassName"] == "longhorn"
    assert body["spec"]["accessModes"] == ["ReadWriteOnce"]


def test_request_volume_labels_the_pvc():
    cluster = MagicMock()
    _ns(cluster).request_volume("data", "5Gi", "longhorn")
    body = cluster.core.create_namespaced_persistent_volume_claim.call_args.kwargs[
        "body"
    ]
    assert body["metadata"]["labels"][constants.JOURNEY_LABEL_KEY] == "true"


def test_run_pod_mounts_the_pvc_when_given():
    cluster = MagicMock()
    _ns(cluster).run_pod("writer", pvc_name="data")
    body = cluster.core.create_namespaced_pod.call_args.kwargs["body"]
    volume = body["spec"]["volumes"][0]
    assert volume["persistentVolumeClaim"]["claimName"] == "data"
    mount = body["spec"]["containers"][0]["volumeMounts"][0]
    assert mount["mountPath"] == "/data"


def test_run_pod_without_a_pvc_declares_no_volumes():
    cluster = MagicMock()
    _ns(cluster).run_pod("plain")
    body = cluster.core.create_namespaced_pod.call_args.kwargs["body"]
    assert body["spec"].get("volumes", []) == []


def _fake_ns(name):
    ns = MagicMock()
    ns.metadata.name = name
    return ns


def test_sweep_deletes_only_labeled_namespaces():
    cluster = MagicMock()
    stale = _fake_ns("nebari-journey-deadbeef")
    cluster.core.list_namespace.return_value = MagicMock(items=[stale])

    result = sweep_stale_namespaces(cluster)

    cluster.core.list_namespace.assert_called_once_with(
        label_selector=f"{constants.JOURNEY_LABEL_KEY}={constants.JOURNEY_LABEL_VALUE}"
    )
    assert result.deleted == ["nebari-journey-deadbeef"]
    assert result.skipped == []
    cluster.core.delete_namespace.assert_called_once_with(
        name="nebari-journey-deadbeef"
    )


def test_sweep_is_a_noop_when_nothing_is_stale():
    cluster = MagicMock()
    cluster.core.list_namespace.return_value = MagicMock(items=[])
    result = sweep_stale_namespaces(cluster)
    assert result.deleted == []
    assert result.skipped == []
    cluster.core.delete_namespace.assert_not_called()


def test_sweep_skips_a_labeled_namespace_whose_name_does_not_match_the_prefix():
    cluster = MagicMock()
    foreign = _fake_ns("some-other-namespace")
    cluster.core.list_namespace.return_value = MagicMock(items=[foreign])

    result = sweep_stale_namespaces(cluster)

    assert result.deleted == []
    assert result.skipped == ["some-other-namespace"]
    cluster.core.delete_namespace.assert_not_called()


def test_sweep_deletes_matching_and_skips_non_matching_in_a_mixed_batch():
    cluster = MagicMock()
    matching = _fake_ns("nebari-journey-cafef00d")
    foreign = _fake_ns("some-other-namespace")
    cluster.core.list_namespace.return_value = MagicMock(items=[matching, foreign])

    result = sweep_stale_namespaces(cluster)

    assert result.deleted == ["nebari-journey-cafef00d"]
    assert result.skipped == ["some-other-namespace"]
    cluster.core.delete_namespace.assert_called_once_with(
        name="nebari-journey-cafef00d"
    )
    for call in cluster.core.delete_namespace.call_args_list:
        assert call.kwargs["name"] != "some-other-namespace"


def _pod(phase, waiting_reason=None, waiting_message=None, ready=False):
    pod = MagicMock()
    pod.status.phase = phase
    pod.status.reason = None
    condition = MagicMock()
    condition.type = "Ready"
    condition.status = "True" if ready else "False"
    pod.status.conditions = [condition]

    if waiting_reason is None:
        pod.status.container_statuses = []
        return pod

    container = MagicMock()
    container.name = "main"
    container.state.waiting.reason = waiting_reason
    container.state.waiting.message = waiting_message
    container.state.terminated = None
    pod.status.container_statuses = [container]
    return pod


def _ns_with_pod(pod):
    cluster = MagicMock()
    cluster.core.read_namespaced_pod.return_value = pod
    return ScratchNamespace(cluster, "nebari-journey-abcd1234")


def test_wait_pod_ready_returns_when_the_pod_is_ready():
    ns = _ns_with_pod(_pod("Running", ready=True))
    assert ns.wait_pod_ready("writer", timeout=1) is None


def test_wait_pod_ready_fails_fast_on_image_pull_backoff():
    """busybox is pulled from Docker Hub on every run. An unpullable image
    used to surface only as a 180s "pod to be ready" timeout naming nothing."""
    ns = _ns_with_pod(
        _pod("Pending", waiting_reason="ImagePullBackOff", waiting_message="429")
    )
    with pytest.raises(RuntimeError) as excinfo:
        ns.wait_pod_ready("writer", timeout=30)
    message = str(excinfo.value)
    assert "ImagePullBackOff" in message
    assert "429" in message


def test_wait_pod_ready_fails_fast_on_a_failed_pod():
    ns = _ns_with_pod(_pod("Failed"))
    with pytest.raises(RuntimeError, match="Failed"):
        ns.wait_pod_ready("writer", timeout=30)


def test_wait_pod_ready_names_the_pod_in_the_failure():
    ns = _ns_with_pod(_pod("Pending", waiting_reason="CrashLoopBackOff"))
    with pytest.raises(RuntimeError, match="nebari-journey-abcd1234/writer"):
        ns.wait_pod_ready("writer", timeout=30)


def test_wait_pod_ready_tolerates_a_transient_image_pull_error():
    """ErrImagePull is the state kubelet passes through before settling into
    ImagePullBackOff. Failing on it would make one retried pull look like an
    outage, so it must time out rather than fail fast."""
    ns = _ns_with_pod(_pod("Pending", waiting_reason="ErrImagePull"))
    with pytest.raises(TimeoutError):
        ns.wait_pod_ready("writer", timeout=0)


def test_wait_pod_ready_tolerates_a_404_immediately_after_creation(monkeypatch):
    """On EKS the API server can briefly not serve a just-created object
    back to a subsequent read. That 404 means "not visible yet", not
    "failed", so it must be treated as not-ready and polled through."""
    monkeypatch.setattr("nebari_journeys.waits.time.sleep", lambda _: None)
    cluster = MagicMock()
    cluster.core.read_namespaced_pod.side_effect = [
        ApiException(status=404),
        _pod("Running", ready=True),
    ]
    ns = ScratchNamespace(cluster, "nebari-journey-abcd1234")
    assert ns.wait_pod_ready("writer", timeout=30) is None


def test_wait_pod_ready_still_raises_on_a_non_404_api_exception():
    cluster = MagicMock()
    cluster.core.read_namespaced_pod.side_effect = ApiException(status=500)
    ns = ScratchNamespace(cluster, "nebari-journey-abcd1234")
    with pytest.raises(ApiException):
        ns.wait_pod_ready("writer", timeout=30)


def test_wait_pod_ready_still_fails_fast_on_a_terminal_phase_after_a_404(monkeypatch):
    """The 404 tolerance must not swallow a genuine terminal failure that
    shows up once the object becomes visible."""
    monkeypatch.setattr("nebari_journeys.waits.time.sleep", lambda _: None)
    cluster = MagicMock()
    cluster.core.read_namespaced_pod.side_effect = [
        ApiException(status=404),
        _pod("Failed"),
    ]
    ns = ScratchNamespace(cluster, "nebari-journey-abcd1234")
    with pytest.raises(RuntimeError, match="Failed"):
        ns.wait_pod_ready("writer", timeout=30)


def test_namespace_sweep_records_a_failed_delete_and_keeps_going():
    """One undeletable namespace must not stop the sweep from clearing the
    rest, and must not vanish from the report."""
    cluster = MagicMock()
    cluster.core.list_namespace.return_value = MagicMock(
        items=[_fake_ns("nebari-journey-aaa"), _fake_ns("nebari-journey-bbb")]
    )
    cluster.core.delete_namespace.side_effect = [
        ApiException(status=500, reason="boom"),
        None,
    ]

    result = sweep_stale_namespaces(cluster)

    assert result.deleted == ["nebari-journey-bbb"]
    assert result.failed == ["nebari-journey-aaa"]


def test_namespace_sweep_treats_an_already_gone_namespace_as_deleted():
    """A 404 means someone else got there first, which is the outcome the
    sweep wanted. Recording it as a failure would make a clean cluster look
    dirty."""
    cluster = MagicMock()
    cluster.core.list_namespace.return_value = MagicMock(
        items=[_fake_ns("nebari-journey-aaa")]
    )
    cluster.core.delete_namespace.side_effect = ApiException(status=404, reason="gone")

    result = sweep_stale_namespaces(cluster)

    assert result.deleted == ["nebari-journey-aaa"]
    assert result.failed == []
