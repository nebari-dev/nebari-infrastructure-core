from unittest.mock import MagicMock

from nebari_journeys import constants
from nebari_journeys.argocd import (
    Application,
    foundational_applications,
    is_foundational,
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
    app = Application.from_object(
        _app("keycloak", conditions=[{"type": "SyncError"}])
    )
    assert app.has_sync_error() is True


def test_unrelated_condition_type_is_not_a_sync_error():
    app = Application.from_object(
        _app("keycloak", conditions=[{"type": "SomeOtherCondition"}])
    )
    assert app.has_sync_error() is False
