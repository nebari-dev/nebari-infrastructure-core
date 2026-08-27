import socket
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from kubernetes.client.rest import ApiException

from nebari_journeys.trust import (
    chromium_args,
    install_dns_mapping,
    needs_dns_mapping,
    trust_anchor_pem,
    write_trust_anchor,
)

CA_PEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
LEAF_PEM = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"


def _cluster_with_tls_secret(data):
    cluster = MagicMock()
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

    cluster = MagicMock()
    cluster.secret_value.side_effect = side_effect
    assert trust_anchor_pem(cluster) == LEAF_PEM


def test_trust_anchor_is_none_when_the_secret_is_absent():
    cluster = MagicMock()
    cluster.secret_value.side_effect = ApiException(status=404, reason="Not Found")
    assert trust_anchor_pem(cluster) is None


def test_trust_anchor_403_propagates_and_names_the_secret():
    cluster = MagicMock()
    cluster.secret_value.side_effect = ApiException(status=403, reason="Forbidden")
    with pytest.raises(RuntimeError) as excinfo:
        trust_anchor_pem(cluster)
    assert "nebari-gateway-tls" in str(excinfo.value)


def test_trust_anchor_connection_error_propagates():
    cluster = MagicMock()
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
    assert "--host-resolver-rules=MAP *.nebari.local 10.0.0.5" in args
