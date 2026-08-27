from unittest.mock import MagicMock

from nebari_journeys import constants
from nebari_journeys.argocd import (
    Application,
    foundational_applications,
    is_foundational,
)

FOUNDATIONAL_LABELS = {constants.PART_OF_LABEL: constants.FOUNDATIONAL_PART_OF}


def _app(name, sync="Synced", health="Healthy", labels=None):
    metadata = {"name": name}
    metadata["labels"] = FOUNDATIONAL_LABELS if labels is None else labels
    return {
        "metadata": metadata,
        "status": {"sync": {"status": sync}, "health": {"status": health}},
    }


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
