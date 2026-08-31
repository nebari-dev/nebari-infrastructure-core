import pathlib
import socket
import ssl as ssl_module
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from kubernetes.client.rest import ApiException

from nebari_journeys import trust
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


def test_chromium_args_pin_the_spki_hash_for_an_untrusted_chain():
    args = chromium_args(
        "nebari.local",
        "10.0.0.5",
        True,
        SELF_SIGNED_LEAF_PEM,
        anchor_trust=trust.SELF_SIGNED_LEAF,
    )
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


# --- the DNS mapping's carve-out ------------------------------------------
#
# install_dns_mapping rebinds socket.getaddrinfo for the whole session and
# redirects every name under the platform domain to the gateway. urllib3
# calls socket.getaddrinfo as a module attribute
# (urllib3/util/connection.py), so the Kubernetes client goes through the
# patch too. On a cluster whose API server lives under the platform domain
# -- plausible on-prem -- that would silently route every subsequent
# Kubernetes API call through Envoy.


def _resolved_host(domain, address, host, exempt_hosts=()):
    """The host install_dns_mapping actually hands to the real resolver."""
    seen = []

    def fake_getaddrinfo(h, port, *args, **kwargs):
        seen.append(h)
        return [(2, 1, 6, "", (h, port))]

    with patch("socket.getaddrinfo", fake_getaddrinfo):
        undo = trust.install_dns_mapping(domain, address, exempt_hosts=exempt_hosts)
        try:
            socket.getaddrinfo(host, 443)
        finally:
            undo()
    return seen[-1]


def test_subdomains_of_the_platform_domain_are_redirected_to_the_gateway():
    assert (
        _resolved_host("nebari.test", "10.0.0.5", "keycloak.nebari.test") == "10.0.0.5"
    )


def test_the_apex_is_redirected_to_the_gateway():
    assert _resolved_host("nebari.test", "10.0.0.5", "nebari.test") == "10.0.0.5"


def test_a_host_outside_the_platform_domain_is_left_alone():
    assert _resolved_host("nebari.test", "10.0.0.5", "example.com") == "example.com"


def test_the_kubernetes_api_host_is_exempt_even_under_the_platform_domain():
    """Without this carve-out every Kubernetes API call after the mapping is
    installed would be sent to the gateway, and the failures would look
    like a broken cluster rather than a hijacked resolver."""
    assert (
        _resolved_host(
            "nebari.test",
            "10.0.0.5",
            "k8s.nebari.test",
            exempt_hosts=("k8s.nebari.test",),
        )
        == "k8s.nebari.test"
    )


def test_the_exemption_is_case_insensitive():
    """DNS names are case insensitive, and a kubeconfig is free to spell the
    API server host however it likes."""
    assert (
        _resolved_host(
            "nebari.test",
            "10.0.0.5",
            "K8S.Nebari.Test",
            exempt_hosts=("k8s.nebari.test",),
        )
        == "K8S.Nebari.Test"
    )


def test_an_exempt_host_outside_the_domain_changes_nothing():
    assert (
        _resolved_host(
            "nebari.test",
            "10.0.0.5",
            "keycloak.nebari.test",
            exempt_hosts=("api.other",),
        )
        == "10.0.0.5"
    )


# --- anchor classification -------------------------------------------------
#
# The suite used to ask one question -- "is this a self-signed leaf?" -- and
# treat "no" as "publicly trusted". That is wrong in both directions:
#
#   * Let's Encrypt STAGING produces a real chain from an untrusted root.
#     It classified as not-self-signed, so test_tls ran with verify=True
#     and FAILED, even though staging is a deliberate CI choice, not a
#     platform defect. Every cloud fixture in .github/fixtures/deploy/
#     uses staging, so this is the shape CI actually hits.
#   * Production ACME also classified as not-self-signed, but because
#     trust_anchor_pem falls back to tls.crt it still returned a PEM, so
#     chromium_args emitted an SPKI pin -- meaning the browser journeys
#     never exercised real certificate validation on the one cluster shape
#     where they could have.

from tests_lib.certgen import make_cert
from tests_lib.certgen import pem as chain_pem


@pytest.fixture(scope="module")
def public_chain():
    """A leaf + intermediate chaining to a root we will put in the store."""
    root, rk = make_cert("Test Public Root", ca=True)
    inter, ik = make_cert("Test Public Intermediate", root, rk, ca=True)
    leaf, _ = make_cert("nebari.test", inter, ik, dns=["nebari.test"])
    return chain_pem(leaf, inter), chain_pem(root)


@pytest.fixture(scope="module")
def private_chain():
    """The Let's Encrypt staging shape: a real chain from a root nobody
    trusts."""
    root, rk = make_cert("(STAGING) Pretend Root", ca=True)
    inter, ik = make_cert("(STAGING) Pretend Intermediate", root, rk, ca=True)
    leaf, _ = make_cert("nebari.test", inter, ik, dns=["nebari.test"])
    return chain_pem(leaf, inter)


def test_a_chain_to_a_trusted_root_is_publicly_trusted(public_chain):
    chain, root = public_chain
    assert (
        trust.classify_anchor(chain, "nebari.test", store_pems=root)
        == trust.PUBLICLY_TRUSTED
    )


def test_a_staging_style_chain_is_privately_issued(private_chain, public_chain):
    """The case CI hits on every cloud provider."""
    _, unrelated_root = public_chain
    assert (
        trust.classify_anchor(private_chain, "nebari.test", store_pems=unrelated_root)
        == trust.PRIVATELY_ISSUED
    )


def test_a_self_signed_leaf_is_still_recognised_as_such(public_chain):
    """cert-manager's selfsigned-issuer output, the kind/local shape."""
    leaf, _ = make_cert("nebari.test", dns=["nebari.test"])
    _, root = public_chain
    assert (
        trust.classify_anchor(chain_pem(leaf), "nebari.test", store_pems=root)
        == trust.SELF_SIGNED_LEAF
    )


def test_no_anchor_at_all_is_publicly_trusted():
    """No gateway secret: the system store is the only thing in play."""
    assert trust.classify_anchor(None, "nebari.test") == trust.PUBLICLY_TRUSTED


def test_an_expired_certificate_from_a_trusted_root_stays_publicly_trusted(
    public_chain,
):
    """Fails CLOSED. An expired cert is a real platform defect; classifying
    it as privately issued would make test_tls SKIP and hide it."""
    root, rk = make_cert("Test Public Root", ca=True)
    inter, ik = make_cert("Test Public Intermediate", root, rk, ca=True)
    expired, _ = make_cert("nebari.test", inter, ik, dns=["nebari.test"], days=-1)
    assert (
        trust.classify_anchor(
            chain_pem(expired, inter), "nebari.test", store_pems=chain_pem(root)
        )
        == trust.PUBLICLY_TRUSTED
    )


def test_a_wrong_hostname_certificate_from_a_trusted_root_stays_publicly_trusted():
    """Same reasoning as expiry: a real defect must not be skipped away."""
    root, rk = make_cert("Test Public Root", ca=True)
    inter, ik = make_cert("Test Public Intermediate", root, rk, ca=True)
    leaf, _ = make_cert("other.test", inter, ik, dns=["other.test"])
    assert (
        trust.classify_anchor(
            chain_pem(leaf, inter), "nebari.test", store_pems=chain_pem(root)
        )
        == trust.PUBLICLY_TRUSTED
    )


def test_chromium_args_pin_a_privately_issued_chain_too():
    """Staging is not self-signed, but Chromium cannot validate it either."""
    args = chromium_args(
        "nebari.local",
        "10.0.0.5",
        False,
        SELF_SIGNED_LEAF_PEM,
        anchor_trust=trust.PRIVATELY_ISSUED,
    )
    assert any(a.startswith("--ignore-certificate-errors-spki-list=") for a in args)


def test_chromium_args_never_pin_a_publicly_trusted_chain():
    """The whole point: on production ACME the browser journeys must run
    with exactly the flags a real user's browser would see. Previously the
    pin was emitted here, because a PEM is always derived."""
    args = chromium_args(
        "nebari.example",
        "10.0.0.5",
        False,
        SELF_SIGNED_LEAF_PEM,
        anchor_trust=trust.PUBLICLY_TRUSTED,
    )
    assert args == []


# --- validity is verifiable without public trust ---------------------------
#
# A staging certificate is a real certificate: real expiry, real SANs, real
# chain, real encryption. Only public trust is unavailable. Skipping the
# whole TLS journey there discarded everything else.


def _serve_tls(chain_pem_text, key, host="127.0.0.1"):
    """A throwaway TLS server presenting a real chain, for handshake tests."""
    import socket as s
    import ssl
    import tempfile
    import threading

    from cryptography.hazmat.primitives import serialization

    d = tempfile.mkdtemp()
    cert_path = pathlib.Path(d) / "chain.pem"
    key_path = pathlib.Path(d) / "key.pem"
    cert_path.write_text(chain_pem_text)
    key_path.write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(str(cert_path), str(key_path))
    sock = s.socket()
    sock.bind((host, 0))
    sock.listen(1)
    port = sock.getsockname()[1]

    def serve():
        try:
            conn, _ = sock.accept()
            with ctx.wrap_socket(conn, server_side=True):
                pass
        except OSError:
            pass

    threading.Thread(target=serve, daemon=True).start()
    return port, sock


def test_a_privately_issued_certificate_still_proves_validity_and_encryption():
    """The Let's Encrypt staging shape. Against the chain the cluster
    itself serves, the handshake still verifies expiry, hostname and chain
    completeness, and reports a real TLS version."""
    root, rk = make_cert("(STAGING) Pretend Root", ca=True)
    inter, ik = make_cert("(STAGING) Pretend Intermediate", root, rk, ca=True)
    leaf, lk = make_cert("localhost", inter, ik, dns=["localhost"])

    import tempfile

    anchor = pathlib.Path(tempfile.mkdtemp()) / "anchor.pem"
    anchor.write_text(chain_pem(leaf, inter))
    port, sock = _serve_tls(chain_pem(leaf, inter), lk)
    try:
        version = trust.negotiated_tls(
            "localhost", "127.0.0.1", port=port, ca_file=str(anchor)
        )
    finally:
        sock.close()
    assert version in trust.ACCEPTABLE_TLS_VERSIONS


def test_an_expired_certificate_fails_the_handshake_even_privately_issued():
    """The signal that skipping used to throw away: staging or not, an
    expired certificate must fail."""
    import tempfile

    root, rk = make_cert("(STAGING) Pretend Root", ca=True)
    inter, ik = make_cert("(STAGING) Pretend Intermediate", root, rk, ca=True)
    expired, ek = make_cert("localhost", inter, ik, dns=["localhost"], days=-1)

    anchor = pathlib.Path(tempfile.mkdtemp()) / "anchor.pem"
    anchor.write_text(chain_pem(expired, inter))
    port, sock = _serve_tls(chain_pem(expired, inter), ek)
    try:
        with pytest.raises(ssl_module.SSLError):
            trust.negotiated_tls(
                "localhost", "127.0.0.1", port=port, ca_file=str(anchor)
            )
    finally:
        sock.close()


def test_a_certificate_for_the_wrong_hostname_fails_even_privately_issued():
    """The other signal skipping threw away."""
    import tempfile

    root, rk = make_cert("(STAGING) Pretend Root", ca=True)
    inter, ik = make_cert("(STAGING) Pretend Intermediate", root, rk, ca=True)
    wrong, wk = make_cert("elsewhere.test", inter, ik, dns=["elsewhere.test"])

    anchor = pathlib.Path(tempfile.mkdtemp()) / "anchor.pem"
    anchor.write_text(chain_pem(wrong, inter))
    port, sock = _serve_tls(chain_pem(wrong, inter), wk)
    try:
        with pytest.raises(ssl_module.SSLCertVerificationError):
            trust.negotiated_tls(
                "localhost", "127.0.0.1", port=port, ca_file=str(anchor)
            )
    finally:
        sock.close()


def test_the_trust_store_survives_one_unparseable_certificate():
    """certifi ships a root with a non-positive serial number, which
    `cryptography` warns will become a hard error. All-or-nothing parsing
    would then discard the whole store, and every publicly trusted cluster
    would be misclassified as privately issued -- silently skipping the
    public-trust journey everywhere."""
    root, _ = make_cert("Good Root", ca=True)
    bundle = (
        chain_pem(root) + "-----BEGIN CERTIFICATE-----\nbm90IGEgY2VydGlmaWNhdGU=\n"
        "-----END CERTIFICATE-----\n"
    )
    loaded = trust.load_trust_store(bundle)
    assert [c.subject.rfc4514_string() for c in loaded] == ["CN=Good Root"]


def test_an_entirely_unusable_trust_store_raises_rather_than_misreporting():
    """An empty store is an environment fault. Reporting it as a cluster
    property would skip the public-trust journey and look like a pass."""
    with pytest.raises(RuntimeError, match="environment fault"):
        trust.load_trust_store("no certificates here")


def test_classification_still_works_against_the_real_certifi_bundle():
    """Guards the deprecation warning becoming a hard error: this walks
    the actual shipped bundle, not a synthetic one."""
    import certifi

    store = pathlib.Path(certifi.where()).read_text()
    assert len(trust.load_trust_store(store)) > 100


def test_a_leaf_without_its_intermediates_is_reported_as_privately_issued():
    """Known limitation, pinned so it is a decision rather than a surprise.

    certifi carries roots, not intermediates, so a secret holding only the
    leaf cannot be chained offline even when a browser would trust it.
    The cost is bounded to losing the public-trust assertion: the always-
    running validity journey still checks expiry, hostname and chain
    completeness on this cluster shape.
    """
    root, rk = make_cert("Test Public Root", ca=True)
    inter, ik = make_cert("Test Public Intermediate", root, rk, ca=True)
    leaf, _ = make_cert("nebari.test", inter, ik, dns=["nebari.test"])
    # Only the leaf, and a store holding the real root: still unverifiable.
    assert (
        trust.classify_anchor(
            chain_pem(leaf), "nebari.test", store_pems=chain_pem(root)
        )
        == trust.PRIVATELY_ISSUED
    )


def test_a_leaf_issued_directly_by_a_trusted_root_still_verifies():
    """The chain-completeness limitation is specific to a MISSING
    intermediate, not to a short chain."""
    root, rk = make_cert("Test Public Root", ca=True)
    leaf, _ = make_cert("nebari.test", root, rk, dns=["nebari.test"])
    assert (
        trust.classify_anchor(
            chain_pem(leaf), "nebari.test", store_pems=chain_pem(root)
        )
        == trust.PUBLICLY_TRUSTED
    )


# --- which hostname the validity journey connects on -----------------------
#
# A Nebari gateway serves more than one certificate on the same address:
# nebari-gateway-tls and the landing page's certificate BOTH claim the bare
# apex, and Envoy chooses by SNI. Connecting on the apex can therefore
# return the landing page's certificate while the anchor came from the
# gateway's. On ACME both are publicly trusted and the mismatch is
# invisible; on a self-signed cluster they are unrelated leaves and the
# handshake fails with "self-signed certificate", which reads as a broken
# platform. Caught by CI on kind, and by the local and existing deployment
# tests, after passing on a production-ACME cluster.


def test_anchor_hostnames_lists_the_certificates_dns_names():
    leaf, _ = make_cert(
        "nebari.test", dns=["nebari.test", "keycloak.nebari.test", "argocd.nebari.test"]
    )
    assert trust.anchor_hostnames(chain_pem(leaf)) == [
        "nebari.test",
        "keycloak.nebari.test",
        "argocd.nebari.test",
    ]


def test_verifiable_hostname_avoids_the_contested_apex():
    """The apex is the one name another certificate also claims."""
    leaf, _ = make_cert("nebari.test", dns=["nebari.test", "keycloak.nebari.test"])
    assert (
        trust.verifiable_hostname(chain_pem(leaf), "nebari.test")
        == "keycloak.nebari.test"
    )


def test_verifiable_hostname_falls_back_to_the_apex_when_it_is_the_only_name():
    leaf, _ = make_cert("nebari.test", dns=["nebari.test"])
    assert trust.verifiable_hostname(chain_pem(leaf), "nebari.test") == "nebari.test"


def test_verifiable_hostname_skips_wildcards():
    """A wildcard is a fine SAN but a poor server_hostname."""
    leaf, _ = make_cert("nebari.test", dns=["*.nebari.test", "keycloak.nebari.test"])
    assert (
        trust.verifiable_hostname(chain_pem(leaf), "nebari.test")
        == "keycloak.nebari.test"
    )


def test_verifiable_hostname_uses_the_domain_when_there_is_no_anchor():
    assert trust.verifiable_hostname(None, "nebari.test") == "nebari.test"


def test_verifiable_hostname_uses_the_domain_when_the_certificate_has_no_sans():
    leaf, _ = make_cert("nebari.test")
    assert trust.verifiable_hostname(chain_pem(leaf), "nebari.test") == "nebari.test"
