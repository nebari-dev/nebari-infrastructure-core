"""Internal certificate journeys.

The platform's promise to a pack: ask the cluster's own CA for a
certificate for your Service, and every pod in the cluster trusts it
through the trust-manager bundle, on every certificate type. The root may
sign only in-cluster Service names (and names under the cluster's domain
on selfsigned clusters, where it also signs the gateway), and no IP
addresses, so it cannot vouch for any other DNS name or IP address.

These run against the Kubernetes API and pods inside the cluster only; they
need no gateway access.
"""

from nebari_journeys import certs

SERVER = "tls-server"
OUTSIDE_SERVER = "outside-server"
OUTSIDE_NAME = "example.com"

# OpenSSL's text for X509_V_ERR_PERMITTED_VIOLATION, as the pinned curl image
# prints it. Matching this rather than any certificate error matters: a client
# that does not trust the root at all also fails with "SSL certificate
# problem", and that must not pass as proof the name constraint holds.
NAME_CONSTRAINT_VIOLATION = "permitted subtree violation"

# curl's CURLE_PEER_FAILED_VERIFICATION: the server's certificate did not
# verify against the CA store curl was given.
CURL_PEER_VERIFICATION_FAILED = 60

HOOK = "hook"
WEBHOOK_DENIED_MESSAGE = "nebari-journey-webhook-denied"


def _curl_failure(url: str, ca_file: str | None, code: int, output: str) -> str:
    return (
        f"url={url} ca_file={ca_file or "curl's default store"} "
        f"curl exit={code} output={output!r}"
    )


def test_pack_gets_service_certificate(scratch_namespace, platform_domain):
    """A pack asks the internal issuer for its Service's names and gets a
    certificate that chains to the cluster's root CA."""
    ns = scratch_namespace

    secret = certs.request_certificate(
        ns, SERVER, certs.service_dns_names(SERVER, ns.name)
    )
    certs.wait_certificate_ready(ns, SERVER)

    expected = f"CN=Nebari root CA ({platform_domain})"
    subject = certs.issued_chain_root_subject(ns, secret)
    assert subject == expected, (
        f"Certificate {ns.name}/{SERVER} from ClusterIssuer "
        f"{certs.INTERNAL_ISSUER} was issued by a CA with subject {subject!r}; "
        f"expected the cluster root CA {expected!r}"
    )


def test_pod_trusts_internal_certificate_through_bundle(scratch_namespace):
    """A pod that mounts the trust bundle verifies a pack's internal
    certificate; a pod without it does not, so the bundle is what does it."""
    ns = scratch_namespace

    secret = certs.request_certificate(
        ns, SERVER, certs.service_dns_names(SERVER, ns.name)
    )
    certs.wait_certificate_ready(ns, SERVER)

    certs.run_tls_server(ns, SERVER, secret)
    certs.run_tls_client(ns, "with-bundle", mount_bundle=True)
    certs.run_tls_client(ns, "without-bundle", mount_bundle=False)
    for pod in (SERVER, "with-bundle", "without-bundle"):
        ns.wait_pod_ready(pod)
    ns.wait_service_endpoint_ready(SERVER)

    url = f"https://{SERVER}.{ns.name}.svc/"

    code, output = certs.https_status(ns, "with-bundle", url, certs.BUNDLE_FILE)
    assert code == 0 and "200" in output, (
        "a pod with the trust bundle mounted could not verify a certificate "
        f"from {certs.INTERNAL_ISSUER}: "
        + _curl_failure(url, certs.BUNDLE_FILE, code, output)
    )

    code, output = certs.https_status(ns, "without-bundle", url, None)
    assert code == CURL_PEER_VERIFICATION_FAILED and "certificate" in output.lower(), (
        "a pod WITHOUT the trust bundle was expected to reject the internal "
        "certificate with a verification error (curl exit "
        f"{CURL_PEER_VERIFICATION_FAILED}), which is what proves the "
        "bundle is doing the trusting: " + _curl_failure(url, None, code, output)
    )


def test_root_rejects_names_outside_constraints(scratch_namespace):
    """The root CA cannot vouch for a name outside its permitted names:
    either cert-manager refuses to issue, or a client that trusts the
    bundle refuses the certificate."""
    ns = scratch_namespace

    secret = certs.request_certificate(ns, OUTSIDE_SERVER, [OUTSIDE_NAME])
    try:
        certs.wait_certificate_ready(ns, OUTSIDE_SERVER)
    except TimeoutError:
        status = certs.certificate_status(ns, OUTSIDE_SERVER)
        assert "constraint" in status.lower(), (
            f"Certificate {ns.name}/{OUTSIDE_SERVER} for {OUTSIDE_NAME} never "
            "became Ready, but not because of the root's name constraints; "
            f"cert-manager reports: {status}"
        )
        return

    certs.run_tls_server(ns, OUTSIDE_SERVER, secret)
    certs.run_tls_client(ns, "with-bundle", mount_bundle=True)
    for pod in (OUTSIDE_SERVER, "with-bundle"):
        ns.wait_pod_ready(pod)
    ns.wait_service_endpoint_ready(OUTSIDE_SERVER)

    url = f"https://{OUTSIDE_NAME}/"
    resolve = f"{OUTSIDE_NAME}:443:{certs.service_ip(ns, OUTSIDE_SERVER)}"
    code, output = certs.https_status(
        ns, "with-bundle", url, certs.BUNDLE_FILE, resolve=resolve
    )
    assert code != 0 and NAME_CONSTRAINT_VIOLATION in output, (
        f"cert-manager issued a certificate for {OUTSIDE_NAME} from "
        f"{certs.INTERNAL_ISSUER}, and a pod trusting the bundle was expected "
        f"to reject it with {NAME_CONSTRAINT_VIOLATION!r}: "
        + _curl_failure(
            f"{url} (resolved via {resolve})", certs.BUNDLE_FILE, code, output
        )
    )


def test_ca_injected_webhook_is_called(scratch_namespace):
    """A pack's admission webhook serves a certificate from the internal
    issuer, cert-manager injects the CA into its configuration, and the API
    server calls it: the webhook's denial reaches the client."""
    ns = scratch_namespace

    secret = certs.request_certificate(ns, HOOK, certs.service_dns_names(HOOK, ns.name))
    certs.wait_certificate_ready(ns, HOOK)
    certs.run_denying_webhook(ns, HOOK, secret, WEBHOOK_DENIED_MESSAGE)
    ns.wait_pod_ready(HOOK)
    # failurePolicy is Fail: dialing a Service with no ready endpoint would
    # surface as a call error, not as the webhook's denial.
    ns.wait_service_endpoint_ready(HOOK)

    manifest = certs.webhook_configuration_manifest(
        f"{ns.name}-{HOOK}", ns.name, HOOK, HOOK, certs.WEBHOOK_TARGET_LABEL
    )
    webhook = certs.install_webhook(ns, manifest)
    # The API server names the denying webhook in its response, and this
    # run's webhook name carries the run-unique namespace, so a stale
    # webhook from another run cannot satisfy the assertion below.
    webhook_name = manifest["webhooks"][0]["name"]
    try:
        certs.wait_ca_injected(ns, webhook)
        certs.wait_webhook_consulted(ns, certs.WEBHOOK_TARGET_LABEL)

        outcome = certs.create_configmap(
            ns, "targeted", {certs.WEBHOOK_TARGET_LABEL: "true"}
        )
        assert (
            outcome is not None
            and outcome.status == 403
            and WEBHOOK_DENIED_MESSAGE in (outcome.body or "")
            and webhook_name in (outcome.body or "")
        ), (
            f"creating a ConfigMap labelled {certs.WEBHOOK_TARGET_LABEL} in "
            f"{ns.name} was expected to be refused with 403 by webhook "
            f"{webhook_name!r} carrying its message "
            f"{WEBHOOK_DENIED_MESSAGE!r}, proving the API "
            "server called the webhook over TLS it verified with the CA "
            f"cert-manager injected from {certs.INTERNAL_ISSUER}; got: "
            f"{certs.describe_outcome(outcome)}"
        )

        outcome = certs.create_configmap(ns, "untargeted", {})
        assert outcome is None, (
            f"a ConfigMap WITHOUT {certs.WEBHOOK_TARGET_LABEL} in {ns.name} is "
            "outside the webhook's objectSelector and should be created "
            f"normally; got: {certs.describe_outcome(outcome)}"
        )
    finally:
        certs.delete_webhook(ns, webhook)
