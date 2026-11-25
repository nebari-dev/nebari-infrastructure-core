"""Tests for the internal certificate verbs. These need no cluster."""

from unittest.mock import MagicMock

import pytest
from kubernetes.client.rest import ApiException

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


@pytest.mark.parametrize("ca_file", ["/etc/nebari-trust/ca-certificates.crt", None])
def test_curl_command_retries_only_transient_connect_errors(ca_file):
    """kube-proxy programs a new ClusterIP shortly after the EndpointSlice is
    ready, so the first dial can be refused or time out. curl retries those,
    but never a certificate verification failure, which is what the
    journeys assert on."""
    command = certs.curl_command("https://tls-server.ns.svc/", ca_file)
    assert "--retry-connrefused" in command
    assert int(command[command.index("--retry") + 1]) > 0
    retry_window = int(command[command.index("--retry-max-time") + 1])
    max_time = int(command[command.index("--max-time") + 1])
    # The last attempt may start just inside the window and run to max-time.
    assert retry_window + max_time < certs.EXEC_TIMEOUT
    assert "--retry-all-errors" not in command


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


class _FakeExecStream:
    """Stands in for kubernetes' WSClient as exec_with_exit_code drives it.

    Like the real one (kubernetes 36.0.3, kubernetes/stream/ws_client.py,
    read_all and returncode), read_all() clears the channel the exit code
    is parsed from, so a caller that reads the output first loses the exit
    code. That is what pins the returncode-before-read_all ordering.
    """

    def __init__(self, returncode, output):
        self._returncode = returncode
        self._output = output
        self.closed = False

    def run_forever(self, timeout=None):
        pass

    @property
    def returncode(self):
        return self._returncode

    def read_all(self):
        self._returncode = None
        return self._output

    def close(self):
        self.closed = True


@pytest.mark.parametrize(
    "returncode,output",
    [
        (0, "200"),
        (60, "curl: (60) SSL certificate problem: unable to get local issuer"),
    ],
)
def test_exec_with_exit_code_returns_the_code_and_output(
    monkeypatch, returncode, output
):
    fake = _FakeExecStream(returncode, output)
    monkeypatch.setattr(certs, "stream", lambda *args, **kwargs: fake)

    result = certs.exec_with_exit_code(_ns(), "client", ["curl", "x"], timeout=5)

    assert result == (returncode, output)
    assert fake.closed


def test_exec_with_exit_code_raises_when_the_command_never_finishes(monkeypatch):
    fake = _FakeExecStream(None, "partial")
    monkeypatch.setattr(certs, "stream", lambda *args, **kwargs: fake)

    with pytest.raises(TimeoutError) as excinfo:
        certs.exec_with_exit_code(_ns(), "client", ["sleep", "999"], timeout=5)

    message = str(excinfo.value)
    assert "nebari-journey-abcd1234/client" in message
    assert "partial" in message
    assert fake.closed


def test_webhook_configuration_manifest():
    manifest = certs.webhook_configuration_manifest(
        "nebari-journey-abcd1234-deny", "ns1", "hook", "hook", "example.test/target"
    )

    assert manifest["apiVersion"] == "admissionregistration.k8s.io/v1"
    assert manifest["kind"] == "ValidatingWebhookConfiguration"
    metadata = manifest["metadata"]
    assert metadata["name"] == "nebari-journey-abcd1234-deny"
    assert metadata["labels"] == JOURNEY_LABEL
    assert metadata["annotations"] == {"cert-manager.io/inject-ca-from": "ns1/hook"}

    [webhook] = manifest["webhooks"]
    assert webhook["clientConfig"] == {
        "service": {
            "name": "hook",
            "namespace": "ns1",
            "port": 443,
            "path": "/validate",
        }
    }
    assert webhook["failurePolicy"] == "Fail"
    assert webhook["sideEffects"] == "None"
    assert webhook["admissionReviewVersions"] == ["v1"]
    assert webhook["namespaceSelector"] == {
        "matchLabels": {"kubernetes.io/metadata.name": "ns1"}
    }
    assert webhook["objectSelector"] == {
        "matchExpressions": [{"key": "example.test/target", "operator": "Exists"}]
    }
    assert webhook["rules"] == [
        {
            "operations": ["CREATE"],
            "apiGroups": [""],
            "apiVersions": ["v1"],
            "resources": ["configmaps"],
            "scope": "Namespaced",
        }
    ]
    # The API server requires a fully qualified webhook name: at least
    # three DNS labels.
    assert len(webhook["name"].split(".")) >= 3


@pytest.mark.parametrize(
    "certificate,namespace,expected",
    [
        ("hook", "ns1", "ns1/hook"),
        ("webhook", "nebari-journey-abcd1234", "nebari-journey-abcd1234/webhook"),
    ],
)
def test_webhook_configuration_manifest_names_the_certificate(
    certificate, namespace, expected
):
    manifest = certs.webhook_configuration_manifest(
        "cfg", namespace, "hook", certificate, "example.test/target"
    )
    annotations = manifest["metadata"]["annotations"]
    assert annotations["cert-manager.io/inject-ca-from"] == expected


def test_denying_webhook_manifests_serve_the_secret_with_the_message():
    pod, service = certs.denying_webhook_manifests("hook", "hook-tls", "denied-x")

    for obj in (pod, service):
        assert obj["metadata"]["name"] == "hook"
        assert obj["metadata"]["labels"][constants.JOURNEY_LABEL_KEY] == "true"
    container = pod["spec"]["containers"][0]
    assert container["image"] == constants.TLS_SERVER_IMAGE
    assert {"name": certs.DENY_MESSAGE_ENV, "value": "denied-x"} in container["env"]
    assert {"secret": {"secretName": "hook-tls"}, "name": "tls"} in pod["spec"][
        "volumes"
    ]
    assert container["readinessProbe"]["tcpSocket"]["port"] == certs.SERVER_PORT
    assert service["spec"]["selector"].items() <= pod["metadata"]["labels"].items()
    assert service["spec"]["ports"] == [
        {"name": "https", "port": 443, "targetPort": certs.SERVER_PORT}
    ]
    script = container["command"][-1]
    assert "do_POST" in script
    assert "allowed=False" in script
    assert "code=403" in script


def test_run_denying_webhook_creates_pod_and_service():
    cluster = MagicMock()
    name = certs.run_denying_webhook(_ns(cluster), "hook", "hook-tls", "denied-x")
    assert name == "hook"
    pod = cluster.core.create_namespaced_pod.call_args.kwargs
    assert pod["namespace"] == "nebari-journey-abcd1234"
    assert pod["body"]["metadata"]["name"] == "hook"
    service = cluster.core.create_namespaced_service.call_args.kwargs
    assert service["namespace"] == "nebari-journey-abcd1234"


def test_install_webhook_creates_it_and_returns_its_name():
    cluster = MagicMock()
    manifest = certs.webhook_configuration_manifest(
        "nebari-journey-abcd1234-deny", "ns1", "hook", "hook", "example.test/target"
    )

    name = certs.install_webhook(_ns(cluster), manifest)

    assert name == "nebari-journey-abcd1234-deny"
    cluster.admission.create_validating_webhook_configuration.assert_called_once_with(
        body=manifest
    )


@pytest.mark.parametrize("status,raises", [(None, False), (404, False), (500, True)])
def test_delete_webhook(status, raises):
    cluster = MagicMock()
    if status is not None:
        cluster.admission.delete_validating_webhook_configuration.side_effect = (
            ApiException(status=status)
        )
    if raises:
        with pytest.raises(ApiException):
            certs.delete_webhook(_ns(cluster), "cfg")
    else:
        certs.delete_webhook(_ns(cluster), "cfg")
    cluster.admission.delete_validating_webhook_configuration.assert_called_once_with(
        name="cfg"
    )


def _webhook_config(*ca_bundles):
    config = MagicMock()
    hooks = []
    for bundle in ca_bundles:
        hook = MagicMock()
        hook.client_config.ca_bundle = bundle
        hooks.append(hook)
    config.webhooks = hooks
    return config


@pytest.mark.parametrize(
    "config,expected",
    [
        (_webhook_config(None), False),
        (_webhook_config(""), False),
        (_webhook_config("LS0t"), True),
        (_webhook_config("LS0t", None), False),
        (_webhook_config(), False),
    ],
)
def test_ca_injected(config, expected):
    assert certs.ca_injected(config) is expected


def test_wait_ca_injected_polls_until_the_bundle_appears(monkeypatch):
    monkeypatch.setattr("nebari_journeys.waits.time.sleep", lambda _: None)
    cluster = MagicMock()
    cluster.admission.read_validating_webhook_configuration.side_effect = [
        _webhook_config(None),
        _webhook_config("LS0t"),
    ]
    certs.wait_ca_injected(_ns(cluster), "cfg", timeout=60)
    assert cluster.admission.read_validating_webhook_configuration.call_count == 2


def test_wait_ca_injected_timeout_names_the_configuration():
    cluster = MagicMock()
    cluster.admission.read_validating_webhook_configuration.return_value = (
        _webhook_config(None)
    )
    with pytest.raises(TimeoutError, match="cfg"):
        certs.wait_ca_injected(_ns(cluster), "cfg", timeout=0)


def test_configmap_manifest_carries_the_journey_labels_and_extras():
    manifest = certs.configmap_manifest("probe", {"example.test/target": "true"})
    assert manifest["metadata"]["name"] == "probe"
    assert manifest["metadata"]["labels"] == {
        **JOURNEY_LABEL,
        "example.test/target": "true",
    }


def test_create_configmap_returns_none_when_admitted():
    cluster = MagicMock()
    assert certs.create_configmap(_ns(cluster), "plain", {}) is None
    kwargs = cluster.core.create_namespaced_config_map.call_args.kwargs
    assert kwargs["namespace"] == "nebari-journey-abcd1234"
    assert "dry_run" not in kwargs


def test_create_configmap_returns_the_rejection():
    cluster = MagicMock()
    rejection = ApiException(status=403, reason="Forbidden")
    cluster.core.create_namespaced_config_map.side_effect = rejection
    assert certs.create_configmap(_ns(cluster), "probe", {}) is rejection


def _api_error(status, body):
    error = ApiException(status=status)
    error.body = body
    return error


DENIED_BODY = '{"message":"admission webhook \\"a.b.c\\" denied the request: x"}'


@pytest.mark.parametrize(
    "outcome,expected",
    [
        (None, False),
        (_api_error(500, "failed calling webhook: x509: unknown authority"), False),
        (_api_error(403, "forbidden: User cannot create configmaps"), False),
        (_api_error(403, DENIED_BODY), True),
        (_api_error(400, DENIED_BODY), True),
    ],
)
def test_denied_by_a_webhook(outcome, expected):
    assert certs.denied_by_a_webhook(outcome) is expected


def test_wait_webhook_consulted_polls_with_dry_run_until_denied(monkeypatch):
    monkeypatch.setattr("nebari_journeys.waits.time.sleep", lambda _: None)
    cluster = MagicMock()
    cluster.core.create_namespaced_config_map.side_effect = [
        None,
        _api_error(403, DENIED_BODY),
    ]

    certs.wait_webhook_consulted(_ns(cluster), "example.test/target", timeout=60)

    calls = cluster.core.create_namespaced_config_map.call_args_list
    assert len(calls) == 2
    for call in calls:
        assert call.kwargs["dry_run"] == "All"
        assert "example.test/target" in call.kwargs["body"]["metadata"]["labels"]


def test_wait_webhook_consulted_timeout_reports_the_last_outcome():
    cluster = MagicMock()
    cluster.core.create_namespaced_config_map.side_effect = _api_error(
        500, "failed calling webhook: x509: certificate signed by unknown authority"
    )
    with pytest.raises(TimeoutError) as excinfo:
        certs.wait_webhook_consulted(_ns(cluster), "example.test/target", timeout=0)
    assert "x509" in str(excinfo.value)
