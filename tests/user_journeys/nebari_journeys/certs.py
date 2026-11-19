"""Internal certificates: what a pack asks the cluster's own CA for.

A pack that serves TLS inside the cluster (an admission webhook, a
metrics endpoint, a database) requests a cert-manager Certificate from
the internal ClusterIssuer and relies on every pod trusting the root
through the trust-manager bundle. These verbs request such a
certificate, serve it, and dial it from a pod with and without the
bundle. Nothing here asserts.

Everything is created inside the scratch namespace and carries the
journey labels, so deleting the namespace removes it and the namespace
sweep catches anything a crashed run left behind.
"""

import textwrap

from cryptography import x509
from kubernetes.stream import stream

from nebari_journeys import constants
from nebari_journeys.cluster import (
    CERTIFICATE_PLURAL,
    CERTMANAGER_GROUP,
    CERTMANAGER_VERSION,
)
from nebari_journeys.k8s import JOURNEY_LABELS, ScratchNamespace
from nebari_journeys.waits import poll_until_settled

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


def tls_server_manifests(name: str, secret: str) -> tuple[dict, dict]:
    """A Pod serving HTTPS from `secret`, and a Service on 443 in front of it."""
    selector = {"app.kubernetes.io/name": name}
    pod = {
        "metadata": {"name": name, "labels": _labels(**selector)},
        "spec": {
            "restartPolicy": "Never",
            "containers": [
                {
                    "name": "server",
                    "image": constants.TLS_SERVER_IMAGE,
                    "command": ["python", "-c", _SERVER_SCRIPT],
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
