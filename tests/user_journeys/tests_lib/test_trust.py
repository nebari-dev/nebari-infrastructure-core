import socket
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from kubernetes.client.rest import ApiException

from nebari_journeys.trust import (
    chromium_args,
    gateway_reachable,
    install_dns_mapping,
    is_self_signed_leaf,
    needs_dns_mapping,
    spki_sha256_b64,
    trust_anchor_pem,
    write_trust_anchor,
)

CA_PEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
LEAF_PEM = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"

# A real, disposable RSA self-signed leaf (subject == issuer, CA:FALSE),
# the exact shape cert-manager's default selfsigned-issuer produces. Its
# SPKI SHA-256 hash below was computed independently with:
#   openssl x509 -in leaf.pem -pubkey -noout \
#     | openssl pkey -pubin -outform der \
#     | openssl dgst -sha256 -binary | base64
SELF_SIGNED_LEAF_PEM = """-----BEGIN CERTIFICATE-----
MIIC+zCCAeOgAwIBAgIUA7tr1aRFNuOvVq7/tknFGZ75c8wwDQYJKoZIhvcNAQEL
BQAwFzEVMBMGA1UEAwwMbmViYXJpLmxvY2FsMB4XDTI2MDgyNzIzNDkzMFoXDTM2
MDgyNDIzNDkzMFowFzEVMBMGA1UEAwwMbmViYXJpLmxvY2FsMIIBIjANBgkqhkiG
9w0BAQEFAAOCAQ8AMIIBCgKCAQEA87tGVT6tyWWopZTB6XOtXcsgiIrODwjqHdqG
ZF2vIcKXutjT/EV34OdkqY1N47jRcDZvNjqOsHQue6FOrPfvRJR2t538M4KwfYNr
jZTVsAc4xmBG8sznbXj0csIA6DKS8yrnPDvsmNdJaFFEPlp5pMy9UbJTmxgFPYQ2
wMuhlP1sIUEYSgM853MP78cWGcsLnfazfVZczyqpHRBkqEspGdOcKPpticKTa57J
iRybcpLM+swsxLC/QhNJgrc8MZ9yxGzxa02pidfp4x3cvWZesgZ+DmwM1WykE+xD
96eYulj8op0fdqBIdl4k724UR5z3O0BJrJDWFAsXQAQK3u+W/QIDAQABoz8wPTAM
BgNVHRMBAf8EAjAAMA4GA1UdDwEB/wQEAwIFoDAdBgNVHQ4EFgQUVDttz8wvWToK
r/Sw4pfMcmSw8ecwDQYJKoZIhvcNAQELBQADggEBADH/rnD1RVZwV7UOgSEAzajT
b78fKJmFcvzXpo8XTNAHilBtP7uKJCa0dktB28dSvJBVg2bvIQVNuWVZmVyOL22B
AqjEAIJIi6lvwUre2Pa8yRTRQggASMBDJVgwzuNDlVvW9HmPl/H86E37sqyh3pbI
llXIkLpYNPrNc5hHVUMW8AIdv9frdpLfCfLueBFiOSXSW2lxFbuSwShCd1574p0c
0e2fH/h/AYf2+pRevKmIty0dDulEZIx/dSb5rBU7qGKqxFvFZPuyy2EeRd9Ab7Vc
LY9jl8awrLzz0nC6293sbFFnI4ZXpeROrND3uuGHPjX7d0tFyLQs0qis5745SRE=
-----END CERTIFICATE-----
"""
SELF_SIGNED_LEAF_SPKI_SHA256_B64 = "wVJrYeL1K09pMlA82Zb55ttHp5Y6ux4+xgbMK1L79SY="

# A real, disposable self-signed CA certificate (subject == issuer,
# CA:TRUE), so is_self_signed_leaf() can be shown to distinguish "self
# signed" from "not a CA" rather than conflating the two.
SELF_SIGNED_CA_PEM = """-----BEGIN CERTIFICATE-----
MIIDDzCCAfegAwIBAgIUP17a+5KxPfWy/PckcSsZmCujAaUwDQYJKoZIhvcNAQEL
BQAwFzEVMBMGA1UEAwwMbmViYXJpLmxvY2FsMB4XDTI2MDgyNzIzNDkyM1oXDTM2
MDgyNDIzNDkyM1owFzEVMBMGA1UEAwwMbmViYXJpLmxvY2FsMIIBIjANBgkqhkiG
9w0BAQEFAAOCAQ8AMIIBCgKCAQEAu5tOy0zURDNX702nf1N0EQnCFfBQx9gHGC6o
+gYfeZxKMitnm1HWhqCtmX9R3ofGbua3to/Jfemq9WyGO9z07Y4F/yNlXunb1tLh
Qcvb5yGWWVQFaLGxALv408YQ5fr9JiSZzP7PX8ORLM/CyEHws8HNQpcWMA1WnNHh
n+G/ZsUoKpEbstgLY5C1eriSEHC6QeRM7lhfzAC4AgAKg2AxGInRbTEm2PhbZacf
Jrvbd6fNgPuxnfXNdoQJsJhEtoUQbWLNIqAYS2/qf4R1F//YYivtUwFPSTFXklvo
1azvRbGUWjQa5+FammVJN/aky4tPFs+TYH/vo7CabhStiOGZMwIDAQABo1MwUTAd
BgNVHQ4EFgQUgx5Xfo1YPHH1f85UbyrfnVnC+gUwHwYDVR0jBBgwFoAUgx5Xfo1Y
PHH1f85UbyrfnVnC+gUwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOC
AQEAgHDcZkY3zkuHJRjyUg1H3zVUZPbZhipA8GTQ4K9fyZTZRiVZIdfEvF98+Wjz
cN6kQnPihaDLeUEneSDpilfZhLifj3c92rZjxNR0ZWfeuNEHvJHxdVtgit4hPm+S
B2DM8pzWAYhsEeyQxSpaKqVO9f5Xfe0WSEHDAQF7gz8DSXQEWytG6JGk7ZtTAIkc
o4+wYZ6AflIL21c9nf1qUt2xEni6MZg1SQR+NOF/r2gFpFFBAObkOOUSz2LbpsDS
oNLuNtVTa3AYzkQ0B0aAO+ZApJn9a8V4OGK0APqvlHe6Ro19AadYDxoidRstmdOM
aQdmcdsYlDhHOk3SuZrKtwQWmA==
-----END CERTIFICATE-----
"""


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


def test_chromium_args_add_no_spki_flag_when_no_anchor_was_derived():
    """A publicly trusted (ACME) chain gets no extra flags at all: Chromium
    must be driven exactly as a real user's browser would be."""
    args = chromium_args("nebari.example", "10.0.0.5", False, None)
    assert args == []


def test_chromium_args_pin_the_spki_hash_when_an_anchor_was_derived():
    args = chromium_args("nebari.local", "10.0.0.5", True, SELF_SIGNED_LEAF_PEM)
    spki_flags = [
        a for a in args if a.startswith("--ignore-certificate-errors-spki-list=")
    ]
    assert spki_flags == [
        f"--ignore-certificate-errors-spki-list={SELF_SIGNED_LEAF_SPKI_SHA256_B64}"
    ]


def test_spki_sha256_b64_matches_a_hash_computed_independently_with_openssl():
    assert spki_sha256_b64(SELF_SIGNED_LEAF_PEM) == SELF_SIGNED_LEAF_SPKI_SHA256_B64


def test_is_self_signed_leaf_true_for_a_ca_false_self_signed_certificate():
    """The exact shape cert-manager's default selfsigned-issuer produces:
    subject == issuer, CA:FALSE."""
    assert is_self_signed_leaf(SELF_SIGNED_LEAF_PEM) is True


def test_is_self_signed_leaf_false_for_a_real_ca_certificate():
    """subject == issuer alone is not enough: a self-signed root CA (CA:TRUE)
    is a legitimate trust anchor and must not be treated as the
    cert-manager selfsigned-issuer leaf shape."""
    assert is_self_signed_leaf(SELF_SIGNED_CA_PEM) is False


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
