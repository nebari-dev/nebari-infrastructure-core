from unittest.mock import MagicMock

from nebari_journeys import constants
from nebari_journeys.argocd import (
    Application,
    all_healthy,
    foundational_applications,
    is_foundational,
    settled_foundational_applications,
)

FOUNDATIONAL_LABELS = {constants.PART_OF_LABEL: constants.FOUNDATIONAL_PART_OF}


def _app(
    name,
    sync="Synced",
    health="Healthy",
    labels=None,
    operation_phase="Succeeded",
    conditions=None,
):
    metadata = {"name": name}
    metadata["labels"] = FOUNDATIONAL_LABELS if labels is None else labels
    status = {
        "sync": {"status": sync},
        "health": {"status": health},
        "operationState": {"phase": operation_phase},
        "conditions": conditions or [],
    }
    return {"metadata": metadata, "status": status}


def test_application_reports_synced_and_healthy():
    app = Application.from_object(_app("keycloak"))
    assert app.name == "keycloak"
    assert app.is_synced() is True
    assert app.is_healthy() is True


def test_application_reports_out_of_sync():
    app = Application.from_object(_app("keycloak", sync="OutOfSync"))
    assert app.is_synced() is False


def test_application_reports_degraded():
    app = Application.from_object(_app("keycloak", health="Degraded"))
    assert app.is_healthy() is False


def test_application_tolerates_a_missing_status_block():
    app = Application.from_object({"metadata": {"name": "fresh"}})
    assert app.sync_status == "Unknown"
    assert app.health_status == "Unknown"
    assert app.is_synced() is False


def test_application_tolerates_a_missing_labels_block():
    assert is_foundational({"metadata": {"name": "unlabelled"}}) is False


def test_foundational_applications_excludes_the_root_app():
    cluster = MagicMock()
    cluster.applications.return_value = [
        _app(constants.ROOT_APP_NAME),
        _app("keycloak"),
    ]
    names = [a.name for a in foundational_applications(cluster)]
    assert names == ["keycloak"]


def test_foundational_applications_returns_every_other_labeled_app():
    cluster = MagicMock()
    cluster.applications.return_value = [_app("keycloak"), _app("cert-manager")]
    assert len(foundational_applications(cluster)) == 2


def test_foundational_applications_ignores_an_operators_own_application():
    """An operator running their own ArgoCD Application in the argocd
    namespace must not fail the smoke journey with a message claiming Nebari's
    foundational software is broken."""
    cluster = MagicMock()
    cluster.applications.return_value = [
        _app("keycloak"),
        _app("operators-own-app", sync="OutOfSync", labels={}),
    ]
    names = [a.name for a in foundational_applications(cluster)]
    assert names == ["keycloak"]


def test_foundational_applications_ignores_an_application_labeled_for_something_else():
    cluster = MagicMock()
    cluster.applications.return_value = [
        _app("keycloak"),
        _app("other", labels={constants.PART_OF_LABEL: "somebody-elses-platform"}),
    ]
    names = [a.name for a in foundational_applications(cluster)]
    assert names == ["keycloak"]


def test_is_foundational_accepts_a_labeled_application():
    assert is_foundational(_app("keycloak")) is True


def test_application_tolerates_a_missing_operation_state_and_conditions():
    """A freshly created Application has no status block at all yet; the
    same tolerant defaults ADR-0017's health gate depends on."""
    app = Application.from_object({"metadata": {"name": "fresh"}})
    assert app.operation_phase is None
    assert app.condition_types == ()
    assert app.has_sync_error() is False


def test_healthy_but_out_of_sync_has_no_sync_error():
    """ArgoCD reports OutOfSync for trivial, insignificant drift while an
    app is genuinely Healthy and working; that must not read as a sync
    error, or the smoke journey would fail on exactly the noise it exists
    to ignore."""
    app = Application.from_object(_app("gateway-config", sync="OutOfSync"))
    assert app.is_synced() is False
    assert app.is_healthy() is True
    assert app.has_sync_error() is False


def test_operation_state_error_phase_is_a_sync_error():
    app = Application.from_object(_app("keycloak", operation_phase="Error"))
    assert app.has_sync_error() is True


def test_operation_state_failed_phase_is_a_sync_error():
    app = Application.from_object(_app("keycloak", operation_phase="Failed"))
    assert app.has_sync_error() is True


def test_operation_state_succeeded_phase_is_not_a_sync_error():
    app = Application.from_object(_app("keycloak", operation_phase="Succeeded"))
    assert app.has_sync_error() is False


def test_comparison_error_condition_is_a_sync_error():
    app = Application.from_object(
        _app("keycloak", conditions=[{"type": "ComparisonError"}])
    )
    assert app.has_sync_error() is True


def test_sync_error_condition_is_a_sync_error():
    app = Application.from_object(_app("keycloak", conditions=[{"type": "SyncError"}]))
    assert app.has_sync_error() is True


def test_unrelated_condition_type_is_not_a_sync_error():
    app = Application.from_object(
        _app("keycloak", conditions=[{"type": "SomeOtherCondition"}])
    )
    assert app.has_sync_error() is False


# --- health convergence ----------------------------------------------------


def test_all_healthy_is_false_for_an_empty_list():
    """No applications at all is its own failure, reported by the caller. If
    an empty list counted as settled the convergence wait would return
    instantly on a cluster where ArgoCD never bootstrapped."""
    assert all_healthy([]) is False


def test_all_healthy_requires_every_application():
    apps = [
        Application.from_object(_app("a", health="Healthy")),
        Application.from_object(_app("b", health="Progressing")),
    ]
    assert all_healthy(apps) is False
    assert all_healthy(apps[:1]) is True


def test_settled_applications_returns_as_soon_as_everything_is_healthy():
    cluster = MagicMock()
    cluster.applications.return_value = [_app("a", health="Healthy")]
    apps = settled_foundational_applications(cluster, timeout=5, interval=0)
    assert [a.name for a in apps] == ["a"]
    assert cluster.applications.call_count == 1


def test_settled_applications_waits_out_a_progressing_cluster():
    """A cluster that finished deploying minutes ago legitimately sits at
    Progressing; the suite is meant to run against any deployed cluster."""
    cluster = MagicMock()
    cluster.applications.side_effect = [
        [_app("a", health="Progressing")],
        [_app("a", health="Progressing")],
        [_app("a", health="Healthy")],
    ]
    apps = settled_foundational_applications(cluster, timeout=5, interval=0)
    assert apps[0].health_status == "Healthy"


def test_settled_applications_returns_the_unhealthy_snapshot_on_timeout():
    """The journey's own assertion names which app is unhealthy and how, so
    the wait must hand back the real state rather than raise."""
    cluster = MagicMock()
    cluster.applications.return_value = [_app("a", health="Degraded")]
    apps = settled_foundational_applications(cluster, timeout=0, interval=0)
    assert apps[0].health_status == "Degraded"
