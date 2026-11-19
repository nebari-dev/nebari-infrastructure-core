"""Tests for the internal certificate verbs. These need no cluster."""

from unittest.mock import MagicMock

import pytest

from nebari_journeys import certs, constants
from nebari_journeys.k8s import ScratchNamespace
from tests_lib.certgen import make_cert, pem

JOURNEY_LABEL = {constants.JOURNEY_LABEL_KEY: constants.JOURNEY_LABEL_VALUE}


def _ns(cluster=None):
    return ScratchNamespace(cluster or MagicMock(), "nebari-journey-abcd1234")


@pytest.mark.parametrize(
    "name,namespace,dns_names,issuer,expected_issuer",
    [
        (
            "hook",
            "ns1",
            ["hook.ns1.svc", "hook.ns1.svc.cluster.local"],
            None,
            "nebari-ca-issuer",
        ),
        ("outside", "ns2", ["example.com"], None, "nebari-ca-issuer"),
        ("other", "ns3", ["a.example.org"], "letsencrypt-issuer", "letsencrypt-issuer"),
    ],
)
def test_certificate_manifest(name, namespace, dns_names, issuer, expected_issuer):
    kwargs = {} if issuer is None else {"issuer": issuer}
    manifest = certs.certificate_manifest(name, namespace, dns_names, **kwargs)

    assert manifest["apiVersion"] == "cert-manager.io/v1"
    assert manifest["kind"] == "Certificate"
    assert manifest["metadata"]["name"] == name
    assert manifest["metadata"]["namespace"] == namespace
    assert manifest["metadata"]["labels"] == JOURNEY_LABEL
    assert manifest["spec"]["issuerRef"] == {
        "kind": "ClusterIssuer",
        "name": expected_issuer,
    }
    assert manifest["spec"]["secretName"] == f"{name}-tls"
    assert manifest["spec"]["dnsNames"] == dns_names
    # The Secret cert-manager writes is labeled too, so a journey that dies
    # before its namespace is deleted leaves nothing unlabeled behind.
    assert manifest["spec"]["secretTemplate"]["labels"] == JOURNEY_LABEL


def test_certificate_manifest_does_not_share_the_callers_list():
    names = ["hook.ns1.svc"]
    manifest = certs.certificate_manifest("hook", "ns1", names)
    names.append("mutated")
    assert manifest["spec"]["dnsNames"] == ["hook.ns1.svc"]


@pytest.mark.parametrize(
    "service,namespace,expected",
    [
        ("hook", "ns1", ["hook.ns1.svc", "hook.ns1.svc.cluster.local"]),
        (
            "tls-server",
            "nebari-journey-abcd1234",
            [
                "tls-server.nebari-journey-abcd1234.svc",
                "tls-server.nebari-journey-abcd1234.svc.cluster.local",
            ],
        ),
    ],
)
def test_service_dns_names(service, namespace, expected):
    assert certs.service_dns_names(service, namespace) == expected


@pytest.mark.parametrize(
    "name,value",
    [
        ("INTERNAL_ISSUER", "nebari-ca-issuer"),
        ("ROOT_CA_SECRET", "nebari-root-ca"),
        ("TRUST_BUNDLE_CONFIGMAP", "nebari-trust-bundle"),
        ("TRUST_BUNDLE_KEY", "ca-certificates.crt"),
    ],
)
def test_names_the_journeys_look_up(name, value):
    assert getattr(certs, name) == value


def test_request_certificate_creates_it_through_custom_objects_and_returns_secret():
    cluster = MagicMock()
    secret = certs.request_certificate(_ns(cluster), "hook", ["hook.ns.svc"])

    assert secret == "hook-tls"
    kwargs = cluster.custom.create_namespaced_custom_object.call_args.kwargs
    assert kwargs["group"] == "cert-manager.io"
    assert kwargs["version"] == "v1"
    assert kwargs["plural"] == "certificates"
    assert kwargs["namespace"] == "nebari-journey-abcd1234"
    assert kwargs["body"]["spec"]["dnsNames"] == ["hook.ns.svc"]


def _cert(*conditions):
    return {"status": {"conditions": list(conditions)}}


@pytest.mark.parametrize(
    "certificate,expected",
    [
        ({}, False),
        ({"status": {}}, False),
        (_cert({"type": "Ready", "status": "False"}), False),
        (_cert({"type": "Issuing", "status": "True"}), False),
        (_cert({"type": "Ready", "status": "True"}), True),
    ],
)
def test_is_ready(certificate, expected):
    assert certs.is_ready(certificate) is expected


@pytest.mark.parametrize(
    "certificate,expected_parts",
    [
        ({}, ["no conditions"]),
        (
            _cert(
                {
                    "type": "Ready",
                    "status": "False",
                    "reason": "DoesNotExist",
                    "message": "Issuing certificate as Secret does not exist",
                },
                {
                    "type": "Issuing",
                    "status": "False",
                    "reason": "Failed",
                    "message": "violates name constraints",
                },
            ),
            [
                "Ready=False",
                "DoesNotExist",
                "Issuing=False",
                "Failed",
                "violates name constraints",
            ],
        ),
    ],
)
def test_describe_conditions(certificate, expected_parts):
    described = certs.describe_conditions(certificate)
    for part in expected_parts:
        assert part in described


def test_wait_certificate_ready_returns_once_ready(monkeypatch):
    cluster = MagicMock()
    cluster.custom.get_namespaced_custom_object.side_effect = [
        _cert({"type": "Ready", "status": "False"}),
        _cert({"type": "Ready", "status": "True"}),
    ]
    monkeypatch.setattr("nebari_journeys.waits.time.sleep", lambda _: None)
    certs.wait_certificate_ready(_ns(cluster), "hook", timeout=60)
    assert cluster.custom.get_namespaced_custom_object.call_count == 2


def test_wait_certificate_ready_timeout_names_the_conditions():
    cluster = MagicMock()
    cluster.custom.get_namespaced_custom_object.return_value = _cert(
        {"type": "Ready", "status": "False", "reason": "Pending", "message": "why"}
    )
    with pytest.raises(TimeoutError) as excinfo:
        certs.wait_certificate_ready(_ns(cluster), "hook", timeout=0)
    message = str(excinfo.value)
    assert "nebari-journey-abcd1234/hook" in message
    assert "Ready=False" in message
    assert "why" in message


def test_subject_of_pem_reads_the_root_subject():
    root, _ = make_cert("Nebari root CA (nebari.local)", ca=True)
    assert certs.subject_of_pem(pem(root)) == "CN=Nebari root CA (nebari.local)"


def test_issued_chain_root_subject_reads_ca_crt():
    root, _ = make_cert("Nebari root CA (example.test)", ca=True)
    cluster = MagicMock()
    cluster.secret_value.return_value = pem(root)

    subject = certs.issued_chain_root_subject(_ns(cluster), "hook-tls")

    assert subject == "CN=Nebari root CA (example.test)"
    cluster.secret_value.assert_called_once_with(
        "nebari-journey-abcd1234", "hook-tls", "ca.crt"
    )


@pytest.mark.parametrize(
    "url,ca_file,resolve,present,absent",
    [
        (
            "https://tls-server.ns.svc/",
            "/etc/nebari-trust/ca-certificates.crt",
            None,
            ["--cacert", "/etc/nebari-trust/ca-certificates.crt"],
            ["--resolve", "--insecure", "-k"],
        ),
        (
            "https://tls-server.ns.svc/",
            None,
            None,
            [],
            ["--cacert", "--resolve", "--insecure", "-k"],
        ),
        (
            "https://example.com/",
            "/etc/nebari-trust/ca-certificates.crt",
            "example.com:443:10.96.0.10",
            ["--cacert", "--resolve", "example.com:443:10.96.0.10"],
            ["--insecure", "-k"],
        ),
    ],
)
def test_curl_command(url, ca_file, resolve, present, absent):
    command = certs.curl_command(url, ca_file, resolve)
    assert command[0] == "curl"
    assert command[-1] == url
    for item in present:
        assert item in command
    for item in absent:
        assert item not in command
    if ca_file is not None:
        assert command[command.index("--cacert") + 1] == ca_file
    if resolve is not None:
        assert command[command.index("--resolve") + 1] == resolve


@pytest.mark.parametrize("mount_bundle", [True, False])
def test_tls_client_manifest(mount_bundle):
    pod = certs.tls_client_manifest("client", mount_bundle)

    assert pod["metadata"]["labels"][constants.JOURNEY_LABEL_KEY] == "true"
    container = pod["spec"]["containers"][0]
    assert container["image"] == constants.TLS_CLIENT_IMAGE
    volumes = pod["spec"].get("volumes", [])
    mounts = container.get("volumeMounts", [])
    if mount_bundle:
        assert volumes == [
            {
                "name": "trust-bundle",
                "configMap": {"name": "nebari-trust-bundle"},
            }
        ]
        assert mounts == [
            {"name": "trust-bundle", "mountPath": "/etc/nebari-trust", "readOnly": True}
        ]
    else:
        assert volumes == []
        assert mounts == []


def test_bundle_file_is_the_projected_key():
    assert certs.BUNDLE_FILE == "/etc/nebari-trust/ca-certificates.crt"


def test_tls_server_manifests_serve_the_secret_on_443():
    pod, service = certs.tls_server_manifests("tls-server", "hook-tls")

    for obj in (pod, service):
        assert obj["metadata"]["name"] == "tls-server"
        assert obj["metadata"]["labels"][constants.JOURNEY_LABEL_KEY] == "true"

    container = pod["spec"]["containers"][0]
    assert container["image"] == constants.TLS_SERVER_IMAGE
    assert {"secret": {"secretName": "hook-tls"}, "name": "tls"} in pod["spec"][
        "volumes"
    ]
    assert container["readinessProbe"]["tcpSocket"]["port"] == certs.SERVER_PORT

    selector = service["spec"]["selector"]
    assert selector.items() <= pod["metadata"]["labels"].items()
    assert service["spec"]["ports"] == [
        {"name": "https", "port": 443, "targetPort": certs.SERVER_PORT}
    ]


def test_run_tls_server_creates_pod_and_service():
    cluster = MagicMock()
    name = certs.run_tls_server(_ns(cluster), "tls-server", "hook-tls")
    assert name == "tls-server"
    assert cluster.core.create_namespaced_pod.call_args.kwargs["namespace"] == (
        "nebari-journey-abcd1234"
    )
    assert cluster.core.create_namespaced_service.call_args.kwargs["namespace"] == (
        "nebari-journey-abcd1234"
    )
