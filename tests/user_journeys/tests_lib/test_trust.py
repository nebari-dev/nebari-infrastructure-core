import socket
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from kubernetes.client.rest import ApiException

from nebari_journeys.trust import (
    chromium_args,
    gateway_reachable,
    install_dns_mapping,
    needs_dns_mapping,
    trust_anchor_pem,
    write_trust_anchor,
)

CA_PEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
LEAF_PEM = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"


DEFAULT_REF = ("nebari-gateway-tls", "envoy-gateway-system")


def _cluster(ref=DEFAULT_REF):
    cluster = MagicMock()
    cluster.gateway_tls_secret_ref.return_value = ref
    return cluster


def _cluster_with_tls_secret(data, ref=DEFAULT_REF):
    cluster = _cluster(ref)
    cluster.secret_value.side_effect = lambda ns, name, key: data[key]
    return cluster


def test_trust_anchor_prefers_ca_crt():
    cluster = _cluster_with_tls_secret({"ca.crt": CA_PEM, "tls.crt": LEAF_PEM})
    assert trust_anchor_pem(cluster) == CA_PEM


def test_trust_anchor_falls_back_to_tls_crt_when_no_ca_crt():
    def side_effect(ns, name, key):
        if key == "ca.crt":
            raise KeyError("ca.crt")
        return LEAF_PEM

    cluster = _cluster()
    cluster.secret_value.side_effect = side_effect
    assert trust_anchor_pem(cluster) == LEAF_PEM


def test_trust_anchor_is_none_when_the_secret_is_absent():
    cluster = _cluster()
    cluster.secret_value.side_effect = ApiException(status=404, reason="Not Found")
    assert trust_anchor_pem(cluster) is None


def test_trust_anchor_403_propagates_and_names_the_secret():
    cluster = _cluster()
    cluster.secret_value.side_effect = ApiException(status=403, reason="Forbidden")
    with pytest.raises(RuntimeError) as excinfo:
        trust_anchor_pem(cluster)
    assert "nebari-gateway-tls" in str(excinfo.value)


def test_trust_anchor_connection_error_propagates():
    cluster = _cluster()
    cluster.secret_value.side_effect = ApiException(
        status=500, reason="Internal Server Error"
    )
    with pytest.raises(RuntimeError):
        trust_anchor_pem(cluster)


def test_write_trust_anchor_returns_none_for_none(tmp_path):
    assert write_trust_anchor(None, tmp_path) is None


def test_write_trust_anchor_writes_the_pem(tmp_path):
    path = write_trust_anchor(CA_PEM, tmp_path)
    assert Path(path).read_text() == CA_PEM


def test_no_mapping_needed_when_dns_already_points_at_the_gateway():
    with patch("socket.getaddrinfo", return_value=[(2, 1, 6, "", ("10.0.0.5", 443))]):
        assert needs_dns_mapping("nebari.example", "10.0.0.5") is False


def test_mapping_needed_when_dns_points_elsewhere():
    with patch("socket.getaddrinfo", return_value=[(2, 1, 6, "", ("9.9.9.9", 443))]):
        assert needs_dns_mapping("nebari.example", "10.0.0.5") is True


def test_mapping_needed_when_dns_does_not_resolve():
    with patch("socket.getaddrinfo", side_effect=socket.gaierror):
        assert needs_dns_mapping("nebari.local", "10.0.0.5") is True


def test_install_dns_mapping_redirects_matching_hosts_and_undoes_cleanly():
    original = socket.getaddrinfo
    undo = install_dns_mapping("nebari.local", "10.0.0.5")
    try:
        infos = socket.getaddrinfo("keycloak.nebari.local", 443)
        assert any(info[4][0] == "10.0.0.5" for info in infos)
    finally:
        undo()
    assert socket.getaddrinfo is original


def test_install_dns_mapping_does_not_capture_lookalike_domains():
    """`nebari.local` must not match `evil-nebari.local`: a substring match
    here would silently redirect unrelated hosts to the gateway."""
    undo = install_dns_mapping("nebari.local", "10.0.0.5")
    try:
        infos = socket.getaddrinfo("evil-nebari.local", 443)
    except socket.gaierror:
        pass  # not resolving at all is fine; it was not mapped
    else:
        assert all(info[4][0] != "10.0.0.5" for info in infos)
    finally:
        undo()


def test_chromium_args_are_empty_when_no_mapping_is_needed():
    assert chromium_args("nebari.example", "10.0.0.5", False) == []


def test_chromium_args_map_the_wildcard_domain():
    args = chromium_args("nebari.local", "10.0.0.5", True)
    assert args[0].startswith("--host-resolver-rules=")
    assert "MAP *.nebari.local 10.0.0.5" in args[0]


def test_trust_anchor_reads_the_secret_the_gateway_actually_references():
    """The gateway's TLS secret name and namespace are both operator
    configurable. Reading the default name on a cluster that renamed it 404s
    and silently degrades to system trust, which is the degradation this
    function promises cannot happen."""
    cluster = _cluster(ref=("operator-supplied-tls", "platform-certs"))
    cluster.secret_value.side_effect = lambda ns, name, key: CA_PEM

    assert trust_anchor_pem(cluster) == CA_PEM
    namespace, name, _ = cluster.secret_value.call_args.args
    assert (name, namespace) == ("operator-supplied-tls", "platform-certs")


def test_trust_anchor_error_names_the_secret_it_actually_read():
    cluster = _cluster(ref=("operator-supplied-tls", "platform-certs"))
    cluster.secret_value.side_effect = ApiException(status=403, reason="Forbidden")
    with pytest.raises(RuntimeError) as excinfo:
        trust_anchor_pem(cluster)
    assert "platform-certs/operator-supplied-tls" in str(excinfo.value)


def test_gateway_reachable_true_when_connect_succeeds():
    with patch("socket.create_connection") as create_connection:
        create_connection.return_value.__enter__ = MagicMock()
        create_connection.return_value.__exit__ = MagicMock(return_value=False)
        assert gateway_reachable("10.0.0.5") is True
    create_connection.assert_called_once_with(("10.0.0.5", 443), timeout=5)


def test_gateway_reachable_false_when_connect_raises_os_error():
    with patch("socket.create_connection", side_effect=OSError("connect timed out")):
        assert gateway_reachable("192.168.1.100") is False


def test_gateway_reachable_uses_the_given_port_and_timeout():
    with patch("socket.create_connection") as create_connection:
        create_connection.return_value.__enter__ = MagicMock()
        create_connection.return_value.__exit__ = MagicMock(return_value=False)
        gateway_reachable("10.0.0.5", port=8443, timeout=2)
    create_connection.assert_called_once_with(("10.0.0.5", 8443), timeout=2)


def test_chromium_args_map_the_bare_apex_as_well_as_the_wildcard():
    """`MAP *.domain` does not match the apex, but install_dns_mapping() maps
    it. The two mapping paths must agree or a journey that reaches the apex
    passes under requests and fails under Chromium."""
    args = chromium_args("nebari.local", "10.0.0.5", True)
    assert len(args) == 1
    assert "MAP nebari.local 10.0.0.5" in args[0]
    assert "MAP *.nebari.local 10.0.0.5" in args[0]
