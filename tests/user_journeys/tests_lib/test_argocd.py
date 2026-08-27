from unittest.mock import MagicMock

from nebari_journeys.argocd import Application, foundational_applications


def _app(name, sync="Synced", health="Healthy"):
    return {
        "metadata": {"name": name},
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


def test_foundational_applications_excludes_the_root_app():
    cluster = MagicMock()
    cluster.applications.return_value = [_app("nebari-root"), _app("keycloak")]
    names = [a.name for a in foundational_applications(cluster)]
    assert names == ["keycloak"]


def test_foundational_applications_returns_every_other_app():
    cluster = MagicMock()
    cluster.applications.return_value = [_app("keycloak"), _app("cert-manager")]
    assert len(foundational_applications(cluster)) == 2
