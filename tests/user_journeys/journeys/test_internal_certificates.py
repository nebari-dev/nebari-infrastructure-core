"""Internal certificate journeys.

The platform's promise to a pack: ask the cluster's own CA for a
certificate for your Service, and every pod in the cluster trusts it
through the trust-manager bundle, on every certificate type. The root is
name-constrained to the cluster's domain and in-cluster Service names, so
it cannot be used to impersonate anything else.

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

    url = f"https://{SERVER}.{ns.name}.svc/"

    code, output = certs.https_status(ns, "with-bundle", url, certs.BUNDLE_FILE)
    assert code == 0 and "200" in output, (
        "a pod with the trust bundle mounted could not verify a certificate "
        f"from {certs.INTERNAL_ISSUER}: "
        + _curl_failure(url, certs.BUNDLE_FILE, code, output)
    )

    code, output = certs.https_status(ns, "without-bundle", url, None)
    assert code != 0 and "certificate" in output.lower(), (
        "a pod WITHOUT the trust bundle was expected to reject the internal "
        "certificate with a verification error, which is what proves the "
        "bundle is doing the trusting: " + _curl_failure(url, None, code, output)
    )


def test_root_rejects_names_outside_constraints(scratch_namespace):
    """The root CA cannot vouch for a name outside the cluster's domain and
    Service names: either cert-manager refuses to issue, or a client that
    trusts the bundle refuses the certificate."""
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
