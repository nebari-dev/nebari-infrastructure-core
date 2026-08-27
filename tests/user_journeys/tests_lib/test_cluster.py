import base64
from unittest.mock import MagicMock

import pytest

from nebari_journeys import constants
from nebari_journeys.cluster import Cluster


def _secret(data: dict[str, str]):
    s = MagicMock()
    s.data = {k: base64.b64encode(v.encode()).decode() for k, v in data.items()}
    return s


def _svc(ip=None, hostname=None):
    ingress = MagicMock()
    ingress.ip = ip
    ingress.hostname = hostname
    svc = MagicMock()
    svc.status.load_balancer.ingress = [ingress]
    item_list = MagicMock()
    item_list.items = [svc]
    return item_list


def _cluster(core=None, custom=None):
    return Cluster(core=core or MagicMock(), custom=custom or MagicMock())


def test_secret_value_is_base64_decoded():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"admin-password": "s3cret"})
    assert _cluster(core).secret_value("keycloak", "x", "admin-password") == "s3cret"


def test_secret_value_raises_a_useful_error_on_missing_key():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"other": "v"})
    with pytest.raises(KeyError, match="admin-password"):
        _cluster(core).secret_value("keycloak", "x", "admin-password")


def test_keycloak_admin_password_reads_the_pinned_secret_and_key():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"admin-password": "pw"})
    assert _cluster(core).keycloak_admin_password() == "pw"
    core.read_namespaced_secret.assert_called_once_with(
        name=constants.KEYCLOAK_ADMIN_SECRET, namespace=constants.KEYCLOAK_NAMESPACE
    )


def test_realm_admin_password_reads_the_pinned_secret_and_key():
    core = MagicMock()
    core.read_namespaced_secret.return_value = _secret({"password": "rpw"})
    assert _cluster(core).realm_admin_password() == "rpw"
    core.read_namespaced_secret.assert_called_once_with(
        name=constants.REALM_ADMIN_SECRET, namespace=constants.KEYCLOAK_NAMESPACE
    )


def test_gateway_address_prefers_ip():
    core = MagicMock()
    core.list_namespaced_service.return_value = _svc(
        ip="10.0.0.5", hostname="lb.example"
    )
    assert _cluster(core).gateway_address() == "10.0.0.5"


def test_gateway_address_falls_back_to_hostname():
    core = MagicMock()
    core.list_namespaced_service.return_value = _svc(hostname="lb.example")
    assert _cluster(core).gateway_address() == "lb.example"


def test_gateway_address_uses_the_pinned_namespace_and_selector():
    core = MagicMock()
    core.list_namespaced_service.return_value = _svc(ip="1.2.3.4")
    _cluster(core).gateway_address()
    core.list_namespaced_service.assert_called_once_with(
        namespace=constants.GATEWAY_NAMESPACE,
        label_selector=constants.GATEWAY_LABEL_SELECTOR,
    )


def test_has_app_is_true_when_present():
    custom = MagicMock()
    custom.list_namespaced_custom_object.return_value = {
        "items": [{"metadata": {"name": "keycloak"}}]
    }
    assert _cluster(custom=custom).has_app("keycloak") is True


def test_has_app_is_false_when_absent():
    custom = MagicMock()
    custom.list_namespaced_custom_object.return_value = {"items": []}
    assert _cluster(custom=custom).has_app("longhorn-backup") is False


def test_require_app_skips_when_absent():
    custom = MagicMock()
    custom.list_namespaced_custom_object.return_value = {"items": []}
    with pytest.raises(Exception) as excinfo:
        _cluster(custom=custom).require_app("longhorn-backup")
    assert "longhorn-backup" in str(excinfo.value)
