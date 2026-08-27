import re
from unittest.mock import MagicMock

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
    body = cluster.core.create_namespaced_persistent_volume_claim.call_args.kwargs["body"]
    assert body["spec"]["resources"]["requests"]["storage"] == "5Gi"
    assert body["spec"]["storageClassName"] == "longhorn"
    assert body["spec"]["accessModes"] == ["ReadWriteOnce"]


def test_request_volume_labels_the_pvc():
    cluster = MagicMock()
    _ns(cluster).request_volume("data", "5Gi", "longhorn")
    body = cluster.core.create_namespaced_persistent_volume_claim.call_args.kwargs["body"]
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


def test_sweep_deletes_only_labeled_namespaces():
    cluster = MagicMock()
    stale = MagicMock()
    stale.metadata.name = "nebari-journey-deadbeef"
    cluster.core.list_namespace.return_value = MagicMock(items=[stale])

    deleted = sweep_stale_namespaces(cluster)

    cluster.core.list_namespace.assert_called_once_with(
        label_selector=f"{constants.JOURNEY_LABEL_KEY}={constants.JOURNEY_LABEL_VALUE}"
    )
    assert deleted == ["nebari-journey-deadbeef"]
    cluster.core.delete_namespace.assert_called_once_with(name="nebari-journey-deadbeef")


def test_sweep_is_a_noop_when_nothing_is_stale():
    cluster = MagicMock()
    cluster.core.list_namespace.return_value = MagicMock(items=[])
    assert sweep_stale_namespaces(cluster) == []
    cluster.core.delete_namespace.assert_not_called()
