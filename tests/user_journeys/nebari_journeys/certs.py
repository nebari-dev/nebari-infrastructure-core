"""Internal certificates: what a pack asks the cluster's own CA for.

A pack that serves TLS inside the cluster (an admission webhook, a
metrics endpoint, a database) requests a cert-manager Certificate from
the internal ClusterIssuer and relies on every pod trusting the root
through the trust-manager bundle. These verbs request such a
certificate, serve it, and dial it from a pod with and without the
bundle. Nothing here asserts.

Everything is created inside the scratch namespace and carries the
journey labels, so deleting the namespace removes it and the namespace
sweep catches anything a crashed run left behind. The one exception is
the ValidatingWebhookConfiguration, which only exists cluster-scoped: it
is labelled too, named after the scratch namespace, deleted by the
journey in a `finally`, and caught by
k8s.sweep_stale_webhook_configurations after a crash.
"""

import textwrap

from cryptography import x509
from kubernetes.client.rest import ApiException
from kubernetes.stream import stream

from nebari_journeys import constants
from nebari_journeys.cluster import (
    CERTIFICATE_PLURAL,
    CERTMANAGER_GROUP,
    CERTMANAGER_VERSION,
)
from nebari_journeys.k8s import JOURNEY_LABELS, ScratchNamespace
from nebari_journeys.waits import poll_until_settled, wait_for_condition

CERTIFICATE_READY_TIMEOUT = constants.CERTIFICATE_READY_TIMEOUT

# Re-exported so a journey reads `certs.INTERNAL_ISSUER`; the values live
# in constants.py, where the Go contract test pins them.
INTERNAL_ISSUER = constants.INTERNAL_ISSUER
ROOT_CA_SECRET = constants.ROOT_CA_SECRET
TRUST_BUNDLE_CONFIGMAP = constants.TRUST_BUNDLE_CONFIGMAP
TRUST_BUNDLE_KEY = constants.TRUST_BUNDLE_KEY

BUNDLE_MOUNT_PATH = "/etc/nebari-trust"
BUNDLE_FILE = f"{BUNDLE_MOUNT_PATH}/{TRUST_BUNDLE_KEY}"

# The server listens on an unprivileged port; its Service exposes 443.
SERVER_PORT = 8443
TLS_MOUNT_PATH = "/tls"

# Bounds on one curl attempt and on the exec that runs it, so an
# unanswered connection reports as a curl failure rather than a hang.
CURL_MAX_TIME = 15
EXEC_TIMEOUT = 60

# kube-proxy programs a new Service's ClusterIP shortly after its
# EndpointSlice turns ready (observed on kind, iptables mode: refused at
# 0.05s, served at 0.11s), so the first dial can be refused or time out.
# curl retries only those transient errors (--retry covers timeouts,
# --retry-connrefused adds ECONNREFUSED); a certificate verification
# failure is not transient and is never retried.
CURL_RETRIES = 10
CURL_RETRY_MAX_TIME = 30

_SERVER_SCRIPT = textwrap.dedent(
    f"""\
    import http.server
    import ssl


    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            body = b"ok\\n"
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)


    server = http.server.ThreadingHTTPServer(("", {SERVER_PORT}), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain("{TLS_MOUNT_PATH}/tls.crt", "{TLS_MOUNT_PATH}/tls.key")
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()
    """
)


# The denying admission webhook. It reuses the HTTPS server's shape (same
# image, port, Secret mount and Service on 443) and answers every
# AdmissionReview with allowed=False and the message from its environment.
# It sets status.code 403 itself: a denial without a code reaches the client
# as 400 (k8s.io/apiserver v0.36.2,
# pkg/admission/plugin/webhook/errors/statuserror.go:36-39, ToStatusErr).
DENY_MESSAGE_ENV = "DENY_MESSAGE"
WEBHOOK_PATH = "/validate"
WEBHOOK_TIMEOUT_SECONDS = 10

# The label a ConfigMap carries to be sent to the journey's webhook. Only
# labelled ConfigMaps in the scratch namespace are matched, so the webhook
# (failurePolicy Fail) never blocks trust-manager writing its bundle there.
WEBHOOK_TARGET_LABEL = "nebari.dev/journey-webhook-target"

INJECT_CA_FROM_ANNOTATION = "cert-manager.io/inject-ca-from"
CA_INJECTED_TIMEOUT = 60
# Bounds the wait for the API server to start consulting a webhook it has
# just been told about.
WEBHOOK_CONSULTED_TIMEOUT = 60
WEBHOOK_PROBE_CONFIGMAP = "webhook-probe"

# Text the API server puts in every webhook denial (statuserror.go:29).
_WEBHOOK_DENIAL = "denied the request"

_WEBHOOK_SCRIPT = textwrap.dedent(
    f"""\
    import http.server
    import json
    import os
    import ssl

    MESSAGE = os.environ["{DENY_MESSAGE_ENV}"]


    class Handler(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            length = int(self.headers["Content-Length"])
            review = json.loads(self.rfile.read(length))
            body = json.dumps(
                dict(
                    apiVersion="admission.k8s.io/v1",
                    kind="AdmissionReview",
                    response=dict(
                        uid=review["request"]["uid"],
                        allowed=False,
                        status=dict(code=403, message=MESSAGE),
                    ),
                )
            ).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)


    server = http.server.ThreadingHTTPServer(("", {SERVER_PORT}), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain("{TLS_MOUNT_PATH}/tls.crt", "{TLS_MOUNT_PATH}/tls.key")
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()
    """
)


def _labels(**extra: str) -> dict:
    return {**JOURNEY_LABELS, **extra}


def secret_name(certificate: str) -> str:
    return f"{certificate}-tls"


def certificate_manifest(
    name: str, namespace: str, dns_names: list[str], issuer: str = INTERNAL_ISSUER
) -> dict:
    """A cert-manager Certificate signed by a ClusterIssuer.

    The issued Secret carries the journey labels too, through
    secretTemplate, so nothing this creates is unlabeled.
    """
    return {
        "apiVersion": f"{CERTMANAGER_GROUP}/{CERTMANAGER_VERSION}",
        "kind": "Certificate",
        "metadata": {"name": name, "namespace": namespace, "labels": _labels()},
        "spec": {
            "secretName": secret_name(name),
            "secretTemplate": {"labels": _labels()},
            "dnsNames": list(dns_names),
            "issuerRef": {"kind": "ClusterIssuer", "name": issuer},
        },
    }


def service_dns_names(service: str, namespace: str) -> list[str]:
    """The two names a client inside the cluster dials a Service by."""
    return [f"{service}.{namespace}.svc", f"{service}.{namespace}.svc.cluster.local"]


def is_ready(certificate: dict) -> bool:
    conditions = certificate.get("status", {}).get("conditions") or []
    return any(
        c.get("type") == "Ready" and c.get("status") == "True" for c in conditions
    )


def describe_conditions(certificate: dict) -> str:
    """Every condition on a Certificate, for an error or assertion message."""
    conditions = certificate.get("status", {}).get("conditions") or []
    if not conditions:
        return "no conditions reported"
    return "; ".join(
        f"{c.get('type')}={c.get('status')} "
        f"(reason={c.get('reason')!r}, message={c.get('message')!r})"
        for c in conditions
    )


def subject_of_pem(pem: str) -> str:
    """RFC 4514 subject of the last certificate in a PEM bundle.

    cert-manager's ca.crt holds the issuing CA; when it holds a chain the
    last certificate is the one closest to the root.
    """
    certificates = x509.load_pem_x509_certificates(pem.encode())
    return certificates[-1].subject.rfc4514_string()


def curl_command(
    url: str, ca_file: str | None, resolve: str | None = None
) -> list[str]:
    """curl that prints only the HTTP status and verifies the server.

    `ca_file=None` leaves curl on its built-in store. There is
    deliberately no way to turn verification off.
    """
    command = [
        "curl",
        "--silent",
        "--show-error",
        "--max-time",
        str(CURL_MAX_TIME),
        "--retry",
        str(CURL_RETRIES),
        "--retry-connrefused",
        "--retry-max-time",
        str(CURL_RETRY_MAX_TIME),
        "--output",
        "/dev/null",
        "--write-out",
        "%{http_code}",
    ]
    if ca_file is not None:
        command += ["--cacert", ca_file]
    if resolve is not None:
        command += ["--resolve", resolve]
    return [*command, url]


def tls_server_manifests(
    name: str, secret: str, script: str = _SERVER_SCRIPT, env: list[dict] | None = None
) -> tuple[dict, dict]:
    """A Pod serving HTTPS from `secret`, and a Service on 443 in front of it.

    `script` is the Python program the Pod runs; by default it answers GET
    with 200.
    """
    selector = {"app.kubernetes.io/name": name}
    pod = {
        "metadata": {"name": name, "labels": _labels(**selector)},
        "spec": {
            "restartPolicy": "Never",
            "containers": [
                {
                    "name": "server",
                    "image": constants.TLS_SERVER_IMAGE,
                    "command": ["python", "-c", script],
                    "env": list(env or []),
                    "ports": [{"containerPort": SERVER_PORT}],
                    # Ready only once the socket listens, so a client
                    # dialing right after wait_pod_ready is not refused.
                    "readinessProbe": {
                        "tcpSocket": {"port": SERVER_PORT},
                        "periodSeconds": 2,
                    },
                    "volumeMounts": [
                        {"name": "tls", "mountPath": TLS_MOUNT_PATH, "readOnly": True}
                    ],
                }
            ],
            "volumes": [{"name": "tls", "secret": {"secretName": secret}}],
        },
    }
    service = {
        "metadata": {"name": name, "labels": _labels()},
        "spec": {
            "selector": selector,
            "ports": [{"name": "https", "port": 443, "targetPort": SERVER_PORT}],
        },
    }
    return pod, service


def tls_client_manifest(name: str, mount_bundle: bool) -> dict:
    """An idle curl Pod, with the trust bundle mounted at BUNDLE_MOUNT_PATH or not."""
    container = {
        "name": "client",
        "image": constants.TLS_CLIENT_IMAGE,
        "command": ["sleep", "3600"],
        "volumeMounts": [],
    }
    volumes: list[dict] = []
    if mount_bundle:
        volumes.append(
            {"name": "trust-bundle", "configMap": {"name": TRUST_BUNDLE_CONFIGMAP}}
        )
        container["volumeMounts"].append(
            {"name": "trust-bundle", "mountPath": BUNDLE_MOUNT_PATH, "readOnly": True}
        )
    return {
        "metadata": {"name": name, "labels": _labels()},
        "spec": {
            "restartPolicy": "Never",
            "containers": [container],
            "volumes": volumes,
        },
    }


def request_certificate(ns: ScratchNamespace, name: str, dns_names: list[str]) -> str:
    """Ask the internal issuer for a certificate; returns its Secret's name."""
    ns.cluster.custom.create_namespaced_custom_object(
        group=CERTMANAGER_GROUP,
        version=CERTMANAGER_VERSION,
        namespace=ns.name,
        plural=CERTIFICATE_PLURAL,
        body=certificate_manifest(name, ns.name, dns_names),
    )
    return secret_name(name)


def read_certificate(ns: ScratchNamespace, name: str) -> dict:
    return ns.cluster.custom.get_namespaced_custom_object(
        group=CERTMANAGER_GROUP,
        version=CERTMANAGER_VERSION,
        namespace=ns.name,
        plural=CERTIFICATE_PLURAL,
        name=name,
    )


def certificate_status(ns: ScratchNamespace, name: str) -> str:
    """What cert-manager currently says about a Certificate."""
    return describe_conditions(read_certificate(ns, name))


def wait_certificate_ready(
    ns: ScratchNamespace, name: str, timeout: float = CERTIFICATE_READY_TIMEOUT
) -> None:
    """Wait for Ready=True; on timeout, raise naming every condition."""
    certificate = poll_until_settled(
        lambda: read_certificate(ns, name), is_ready, timeout=timeout
    )
    if not is_ready(certificate):
        raise TimeoutError(
            f"timed out after {timeout:g}s waiting for Certificate "
            f"{ns.name}/{name} to be Ready: {describe_conditions(certificate)}"
        )


def issued_chain_root_subject(ns: ScratchNamespace, secret: str) -> str:
    """Subject of the CA cert-manager wrote into the Secret's ca.crt."""
    return subject_of_pem(ns.cluster.secret_value(ns.name, secret, "ca.crt"))


def run_tls_server(ns: ScratchNamespace, name: str, secret: str) -> str:
    """Start the HTTPS server Pod and its Service, both named `name`."""
    pod, service = tls_server_manifests(name, secret)
    ns.cluster.core.create_namespaced_pod(namespace=ns.name, body=pod)
    ns.cluster.core.create_namespaced_service(namespace=ns.name, body=service)
    return name


def service_ip(ns: ScratchNamespace, name: str) -> str:
    service = ns.cluster.core.read_namespaced_service(name=name, namespace=ns.name)
    return service.spec.cluster_ip


def run_tls_client(ns: ScratchNamespace, name: str, mount_bundle: bool) -> str:
    ns.cluster.core.create_namespaced_pod(
        namespace=ns.name, body=tls_client_manifest(name, mount_bundle)
    )
    return name


def exec_with_exit_code(
    ns: ScratchNamespace, pod: str, command: list[str], timeout: float = EXEC_TIMEOUT
) -> tuple[int, str]:
    """Run a command in a pod; return its exit code and combined output.

    The checked counterpart to ScratchNamespace.exec, built the way that
    method's docstring asks. The exit code must be read before read_all():
    read_all() also clears the error channel the exit code is parsed from
    (kubernetes 36.0.3, kubernetes/stream/ws_client.py, read_all and
    returncode).
    """
    response = stream(
        ns.cluster.core.connect_get_namespaced_pod_exec,
        pod,
        ns.name,
        command=command,
        stderr=True,
        stdin=False,
        stdout=True,
        tty=False,
        _preload_content=False,
    )
    try:
        response.run_forever(timeout=timeout)
        code = response.returncode
        output = response.read_all()
    finally:
        response.close()
    if code is None:
        raise TimeoutError(
            f"command {command!r} in pod {ns.name}/{pod} did not finish within "
            f"{timeout:g}s; output so far: {output!r}"
        )
    return code, output


def https_status(
    ns: ScratchNamespace,
    client: str,
    url: str,
    ca_file: str | None,
    resolve: str | None = None,
) -> tuple[int, str]:
    """curl's exit code and output (the HTTP status, or curl's error)."""
    return exec_with_exit_code(ns, client, curl_command(url, ca_file, resolve))


def webhook_configuration_manifest(
    name: str, namespace: str, service: str, certificate: str, label: str
) -> dict:
    """A ValidatingWebhookConfiguration whose CA cert-manager injects.

    It has no caBundle of its own: the inject-ca-from annotation asks
    cert-manager's cainjector to copy in the CA of `certificate`. It
    matches only ConfigMap creates in `namespace` that carry `label`, and
    fails closed, so a webhook the API server cannot call blocks the
    request instead of letting it through.
    """
    return {
        "apiVersion": "admissionregistration.k8s.io/v1",
        "kind": "ValidatingWebhookConfiguration",
        "metadata": {
            "name": name,
            "labels": _labels(),
            "annotations": {INJECT_CA_FROM_ANNOTATION: f"{namespace}/{certificate}"},
        },
        "webhooks": [
            {
                "name": f"{service}.{namespace}.journey.nebari.dev",
                "clientConfig": {
                    "service": {
                        "name": service,
                        "namespace": namespace,
                        "port": 443,
                        "path": WEBHOOK_PATH,
                    }
                },
                "rules": [
                    {
                        "operations": ["CREATE"],
                        "apiGroups": [""],
                        "apiVersions": ["v1"],
                        "resources": ["configmaps"],
                        "scope": "Namespaced",
                    }
                ],
                "namespaceSelector": {
                    "matchLabels": {"kubernetes.io/metadata.name": namespace}
                },
                "objectSelector": {
                    "matchExpressions": [{"key": label, "operator": "Exists"}]
                },
                "failurePolicy": "Fail",
                "sideEffects": "None",
                "admissionReviewVersions": ["v1"],
                "timeoutSeconds": WEBHOOK_TIMEOUT_SECONDS,
            }
        ],
    }


def denying_webhook_manifests(
    name: str, secret: str, message: str
) -> tuple[dict, dict]:
    """A Pod answering every AdmissionReview with a denial, and its Service."""
    return tls_server_manifests(
        name,
        secret,
        script=_WEBHOOK_SCRIPT,
        env=[{"name": DENY_MESSAGE_ENV, "value": message}],
    )


def run_denying_webhook(
    ns: ScratchNamespace, name: str, secret: str, message: str
) -> str:
    """Start the denying webhook Pod and its Service, both named `name`."""
    pod, service = denying_webhook_manifests(name, secret, message)
    ns.cluster.core.create_namespaced_pod(namespace=ns.name, body=pod)
    ns.cluster.core.create_namespaced_service(namespace=ns.name, body=service)
    return name


def install_webhook(ns: ScratchNamespace, manifest: dict) -> str:
    """Create a ValidatingWebhookConfiguration; returns its name.

    It is cluster-scoped, so deleting the scratch namespace does not remove
    it: the caller deletes it with delete_webhook in a `finally`, and
    k8s.sweep_stale_webhook_configurations catches a crashed run's.
    """
    ns.cluster.admission.create_validating_webhook_configuration(body=manifest)
    return manifest["metadata"]["name"]


def delete_webhook(ns: ScratchNamespace, name: str) -> None:
    try:
        ns.cluster.admission.delete_validating_webhook_configuration(name=name)
    except ApiException as exc:
        if exc.status != 404:
            raise


def ca_injected(config) -> bool:
    """True when every webhook in the configuration has a caBundle."""
    webhooks = config.webhooks or []
    return bool(webhooks) and all(w.client_config.ca_bundle for w in webhooks)


def wait_ca_injected(
    ns: ScratchNamespace, name: str, timeout: float = CA_INJECTED_TIMEOUT
) -> None:
    """Wait for cert-manager to write the caBundle into the configuration."""
    wait_for_condition(
        lambda: ca_injected(
            ns.cluster.admission.read_validating_webhook_configuration(name=name)
        ),
        timeout=timeout,
        description=(
            f"cert-manager to inject a caBundle into ValidatingWebhookConfiguration "
            f"{name} (annotation {INJECT_CA_FROM_ANNOTATION})"
        ),
    )


def configmap_manifest(name: str, labels: dict[str, str]) -> dict:
    return {
        "metadata": {"name": name, "labels": {**JOURNEY_LABELS, **labels}},
        "data": {"journey": "webhook"},
    }


def create_configmap(
    ns: ScratchNamespace, name: str, labels: dict[str, str], dry_run: bool = False
) -> ApiException | None:
    """Create a ConfigMap; returns the API's rejection, or None if admitted."""
    kwargs = {"dry_run": "All"} if dry_run else {}
    try:
        ns.cluster.core.create_namespaced_config_map(
            namespace=ns.name, body=configmap_manifest(name, labels), **kwargs
        )
    except ApiException as exc:
        return exc
    return None


def denied_by_a_webhook(outcome: ApiException | None) -> bool:
    """True when the API server rejected a request on a webhook's answer."""
    return outcome is not None and _WEBHOOK_DENIAL in (outcome.body or "")


def describe_outcome(outcome: ApiException | None) -> str:
    if outcome is None:
        return "admitted"
    return f"HTTP {outcome.status}: {outcome.body}"


def wait_webhook_consulted(
    ns: ScratchNamespace, label: str, timeout: float = WEBHOOK_CONSULTED_TIMEOUT
) -> None:
    """Wait until the API server sends a labelled ConfigMap to a webhook.

    The API server learns about a new or newly injected configuration
    through its own watch, so for a moment after wait_ca_injected it may
    still admit the request without asking, or call with the configuration
    as it was before the caBundle landed. This probes with dry-run creates,
    which reach validating webhooks declaring sideEffects None but persist
    nothing, until one comes back denied by a webhook.
    """
    outcome = poll_until_settled(
        lambda: create_configmap(
            ns, WEBHOOK_PROBE_CONFIGMAP, {label: "true"}, dry_run=True
        ),
        denied_by_a_webhook,
        timeout=timeout,
    )
    if not denied_by_a_webhook(outcome):
        raise TimeoutError(
            f"timed out after {timeout:g}s waiting for the API server to send a "
            f"ConfigMap labelled {label} in {ns.name} to a webhook; last "
            f"outcome: {describe_outcome(outcome)}"
        )
