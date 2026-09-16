"""Trust anchor derivation and name resolution.

TLS verification is never disabled in this suite. Where the platform
domain does not resolve to the gateway, names are mapped for both the
Python client and Chromium, which preserves real SNI and real
certificate validation rather than bypassing it.

The honest trade, spelled out (see issues #447 and #490): cert-manager's
default `selfsigned-issuer` issues a self-signed LEAF certificate
(`CA:FALSE`), not a proper root CA. `requests`/OpenSSL happily accepts a
self-signed end-entity certificate as a trust anchor, which is why the
API journeys work by pointing `verify=` at it directly. Chromium/NSS
does not: a `CA:FALSE` certificate cannot be installed as a trust
anchor at all, so no NSS trust-store trick makes Chromium accept it.

The fix implemented here for the browser journeys is
`--ignore-certificate-errors-spki-list=<hash>`, computed by
`spki_sha256_b64()`: it pins Chromium's trust to the exact public key
(the SubjectPublicKeyInfo) read from the cluster's own gateway secret
over the kubeconfig, and Chromium skips certificate errors ONLY for
connections presenting that exact key. This is deliberately narrower
than full chain validation:

* A different certificate (a MITM presenting any other key, or the
  gateway rotating to a new key without updating the pin) is still
  rejected outright. In that sense the anchor is not weakened at all.
* But for a connection that DOES present the pinned key, Chromium
  suppresses every certificate error on that connection, including
  ones a full chain validation would still catch, such as a hostname
  mismatch. The pin authenticates "this is the key I read from the
  cluster", not "this certificate is a valid one for this hostname".

This is strictly worse than driving a real CA-issued chain through
unmodified Chromium, which is why a PUBLICLY TRUSTED chain gets no extra
flags at all (see `chromium_args` and `classify_anchor`). Note the
condition is public trust, not "is this ACME": Let's Encrypt STAGING is
ACME and is NOT publicly trusted, and every cloud fixture in
.github/fixtures/deploy/ uses staging. The real fix is issue #447:
once cert-manager issues a proper CA for the gateway, Chromium can
validate the chain like any other browser, and this SPKI pin -- along
with `spki_sha256_b64` and the flag it feeds -- should be removed.
"""

import base64
import hashlib
import socket
import ssl
import warnings
from collections.abc import Callable, Iterable
from pathlib import Path

import certifi
from cryptography import x509
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
from cryptography.x509.verification import PolicyBuilder, Store, VerificationError
from kubernetes.client.rest import ApiException

CA_KEY = "ca.crt"
LEAF_KEY = "tls.crt"

NOT_FOUND_STATUS = 404

# How the gateway's certificate relates to the trust the rest of the world
# has. Three outcomes, not two: the suite used to ask only "is this a
# self-signed leaf?", which silently lumped a real-but-untrusted chain in
# with a publicly trusted one and got both of them wrong. See
# `classify_anchor`.
PUBLICLY_TRUSTED = "publicly-trusted"
PRIVATELY_ISSUED = "privately-issued"
SELF_SIGNED_LEAF = "self-signed-leaf"


def trust_anchor_pem(cluster) -> str | None:
    """PEM to verify the gateway against, or None to use the system store.

    The secret is the one the Gateway's own listener references
    (`cluster.gateway_tls_secret_ref()`), NOT a hardcoded
    `nebari-gateway-tls` in `envoy-gateway-system`. Both the name and the
    namespace are operator-configurable via `certificate.secret_name` and
    `certificate.existing_secret` (pkg/config/config.go,
    CertificateConfig.GatewaySecretRef), and ADR-0017 requires the anchor to
    follow the operator's secret. Reading the default name on a cluster that
    renamed it would 404, return None, and silently degrade to system trust,
    which is exactly the degradation this function promises cannot happen.

    Prefers ca.crt (present when an issuing CA exists), falls back to
    tls.crt (a selfSigned issuer produces a self-signed leaf, which is
    its own anchor).

    A missing *key* on an existing secret (KeyError) is expected while
    probing ca.crt before tls.crt, and is not an error. A missing
    *secret* (ApiException 404) is also a legitimate cluster shape: an
    operator supplying their own certificate outside cert-manager may
    have no secret at that name at all, so this returns None and the
    system trust store is used, the normal case for a publicly trusted
    ACME certificate. Any other API error (RBAC denial, connection
    failure, ...) is a real problem the operator needs to see, so it is
    raised rather than silently downgraded to a confusing TLS failure
    later.
    """
    name, namespace = cluster.gateway_tls_secret_ref()

    for key in (CA_KEY, LEAF_KEY):
        try:
            pem = cluster.secret_value(namespace, name, key)
        except KeyError:
            continue
        except ApiException as error:
            if error.status == NOT_FOUND_STATUS:
                return None
            raise RuntimeError(
                f"could not read {key!r} from secret {namespace}/{name}: {error}"
            ) from error
        if pem and pem.strip():
            return pem
    return None


def is_self_signed_leaf(pem: str) -> bool:
    """True when `pem` is a self-signed, non-CA end-entity certificate.

    This is exactly the shape cert-manager's default `selfsigned-issuer`
    produces: `subject == issuer`, and Basic Constraints says `CA:FALSE`
    (or the extension is absent, which defaults to not-a-CA). A real CA
    certificate -- even a self-signed root, where subject also equals
    issuer -- carries `CA:TRUE` and returns False here.

    Used to decide whether `tls`-marked journeys (whose SUBJECT is
    certificate validity or chain trust itself) should be skipped: those
    journeys cannot pass against a leaf that was never meant to be a
    trust anchor, and the SPKI pin `chromium_args` applies for the rest of
    the suite does not change that, it only lets everything else proceed.
    """
    cert = x509.load_pem_x509_certificate(pem.encode())
    if cert.subject != cert.issuer:
        return False
    try:
        basic_constraints = cert.extensions.get_extension_for_class(
            x509.BasicConstraints
        ).value
    except x509.ExtensionNotFound:
        return True
    return not basic_constraints.ca


def load_chain(pem: str) -> list[x509.Certificate]:
    """Every certificate in a PEM bundle, leaf first.

    cert-manager writes the FULL chain into `tls.crt` (on this cluster:
    leaf, then two intermediates), not just the end-entity certificate,
    which is what makes the offline verification in `classify_anchor`
    possible without ever contacting the gateway.
    """
    return list(x509.load_pem_x509_certificates(pem.encode()))


PEM_CERT_START = "-----BEGIN CERTIFICATE-----"


def load_trust_store(store_pems: str) -> list[x509.Certificate]:
    """Parse a CA bundle, skipping any certificate that will not load.

    Deliberately block-by-block rather than one
    `load_pem_x509_certificates` call over the whole bundle, which is
    all-or-nothing. certifi ships at least one root with a non-positive
    serial number -- disallowed by RFC 5280 -- and `cryptography`
    currently warns that "loading this certificate will cause an exception
    in a future release". A single hard failure there would take the
    entire trust store with it, and `classify_anchor` would then report
    every publicly trusted cluster as privately issued, silently skipping
    the public-trust journey everywhere. Dropping one malformed root out
    of certifi's ~150 is the far smaller loss: a root that will not parse
    could not have served as a trust anchor anyway.

    Raises when NOTHING loads: an empty trust store is a broken
    environment, not a fact about the cluster, and must not be reported as
    one.
    """
    certs = []
    for block in store_pems.split(PEM_CERT_START)[1:]:
        pem = PEM_CERT_START + block
        try:
            with warnings.catch_warnings():
                warnings.simplefilter("ignore")
                certs.append(x509.load_pem_x509_certificate(pem.encode()))
        # Broad, and silent: one unparseable root must not discard the
        # store (BLE001), and this module is library code, which reports
        # through return values rather than logging (S112). The empty-store
        # check below is what surfaces a store that is actually broken.
        except Exception:  # noqa: BLE001, S112
            continue
    if not certs:
        raise RuntimeError(
            "no usable certificates in the trust store; this is an "
            "environment fault, not a property of the cluster under test"
        )
    return certs


def _store_subjects(store_pems: str) -> set[bytes]:
    return {cert.subject.public_bytes() for cert in load_trust_store(store_pems)}


def classify_anchor(
    pem: str | None, domain: str, *, store_pems: str | None = None
) -> str:
    """How the gateway's certificate stands relative to public trust.

    Answers the question the suite actually needs and previously could
    not: not "did we read a certificate off the cluster" (we always do --
    `trust_anchor_pem` falls back to `tls.crt`, so it returns a PEM on
    every cert-manager cluster) but "would an ordinary client, and an
    ordinary third-party server, trust this chain?"

    Three outcomes:

    - PUBLICLY_TRUSTED: the chain validates against the public trust
      store, which is the case for production ACME. Nothing needs
      relaxing anywhere: `verify=True` works, Chromium needs no flags,
      and a third party such as ArgoCD's server can complete OIDC
      discovery against the gateway.
    - PRIVATELY_ISSUED: a genuine chain from a CA nothing trusts. Let's
      Encrypt STAGING is exactly this, and every cloud fixture in
      `.github/fixtures/deploy/` uses staging, so this is the shape CI
      hits. Journeys whose subject is public trust must SKIP here: the
      failure is a deliberate CI choice, not a platform defect.
    - SELF_SIGNED_LEAF: cert-manager's default `selfsigned-issuer` output,
      a `CA:FALSE` certificate that is its own issuer (see the module
      docstring).

    Verification is entirely OFFLINE, against certifi's bundle, using the
    chain the cluster secret already contains. That is deliberate: probing
    the gateway over TLS would answer the same question but would make
    every journey depend on gateway reachability, which is precisely what
    the non-autouse `dns_mapping`/`gateway_reachable` split exists to
    avoid.

    An expired or wrong-hostname certificate from a PUBLIC root is
    reported as PUBLICLY_TRUSTED, not PRIVATELY_ISSUED, even though
    verification fails. This is deliberate and it fails CLOSED: those are
    real platform defects, and classifying them as privately issued would
    make the public-trust journey skip and hide them. The root's
    trustworthiness is re-checked by issuer DN so the distinction
    survives.

    KNOWN LIMITATION. A secret holding only the leaf, with no
    intermediates, cannot be verified offline: certifi carries roots, not
    intermediates, so there is nothing to chain through and the issuer-DN
    fallback cannot match either. Such a certificate is reported as
    PRIVATELY_ISSUED even if a browser (which receives the chain from the
    server, not from this secret) would trust it, so the public-trust
    journey skips with a loud reason. cert-manager always writes the full
    chain into `tls.crt`, so this only arises for an operator-supplied
    `certificate.existing_secret`.

    What that costs is bounded, and only because the TLS journeys are
    split: `test_gateway_serves_a_valid_certificate_for_this_domain` runs
    on every cluster shape regardless of this classification and still
    checks expiry, hostname and chain completeness. So a mis-classification
    here loses the "a stranger would trust this" assertion and nothing
    else -- it can never hide a certificate defect.

    `store_pems` overrides the trust store, for tests.
    """
    if pem is None:
        # No secret at all: the system store is the only thing in play, so
        # the chain either works for everyone or fails for everyone.
        return PUBLICLY_TRUSTED

    chain = load_chain(pem)
    if not chain:
        return PUBLICLY_TRUSTED
    if len(chain) == 1 and is_self_signed_leaf(pem):
        return SELF_SIGNED_LEAF

    if store_pems is None:
        store_pems = Path(certifi.where()).read_text()

    leaf, intermediates = chain[0], chain[1:]
    verifier = (
        PolicyBuilder()
        .store(Store(load_trust_store(store_pems)))
        .build_server_verifier(x509.DNSName(domain))
    )
    try:
        verifier.verify(leaf, intermediates)
        return PUBLICLY_TRUSTED
    except VerificationError:
        pass

    # Verification failed. Distinguish "nobody trusts this root" from
    # "trusted root, but the certificate is expired or for the wrong
    # name": only the former justifies skipping the TLS journeys.
    subjects = _store_subjects(store_pems)
    top_issuer = chain[-1].issuer.public_bytes()
    if top_issuer in subjects or chain[-1].subject.public_bytes() in subjects:
        return PUBLICLY_TRUSTED
    return PRIVATELY_ISSUED


def anchor_hostnames(pem: str) -> list[str]:
    """DNS names the anchor certificate claims, in certificate order."""
    cert = load_chain(pem)[0]
    try:
        san = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    except x509.ExtensionNotFound:
        return []
    return list(san.get_values_for_type(x509.DNSName))


def verifiable_hostname(pem: str | None, domain: str) -> str:
    """A hostname on which the GATEWAY's own certificate will be served.

    Not simply the platform domain, and the difference is not academic. A
    Nebari cluster has more than one certificate on the same gateway
    address: alongside `nebari-gateway-tls` there is a landing-page
    certificate, and BOTH claim the bare apex. Which one Envoy serves for
    a given connection is chosen by SNI, so connecting on the apex can
    return the landing page's certificate while the anchor was derived
    from the gateway's. On an ACME cluster both are publicly trusted and
    the mismatch is invisible; on a self-signed cluster they are two
    unrelated self-signed leaves and verification fails with
    "self-signed certificate" -- which looks like a broken platform and
    is really the wrong certificate having been asked for.

    So prefer a name the derived certificate claims that is NOT the bare
    apex, since the apex is the contested one. Falls back to the platform
    domain when the anchor has no other name, or when there is no anchor
    at all.
    """
    if pem is None:
        return domain
    names = [n for n in anchor_hostnames(pem) if not n.startswith("*")]
    specific = [n for n in names if n != domain]
    if specific:
        return specific[0]
    return names[0] if names else domain


def spki_sha256_b64(pem: str) -> str:
    """Base64-encoded SHA-256 hash of a certificate's SubjectPublicKeyInfo.

    This is the value Chromium's `--ignore-certificate-errors-spki-list`
    flag expects. Computed the same way the flag's own documentation
    describes: parse the certificate, take the DER encoding of its
    SubjectPublicKeyInfo (not the whole certificate, and not just the raw
    key bits), hash it with SHA-256, and base64-encode the digest.

    Equivalent to:
        openssl x509 -in cert.pem -pubkey -noout \\
          | openssl pkey -pubin -outform der \\
          | openssl dgst -sha256 -binary | base64
    """
    cert = x509.load_pem_x509_certificate(pem.encode())
    spki_der = cert.public_key().public_bytes(
        Encoding.DER, PublicFormat.SubjectPublicKeyInfo
    )
    digest = hashlib.sha256(spki_der).digest()
    return base64.b64encode(digest).decode("ascii")


def write_trust_anchor(pem: str | None, directory) -> str | None:
    """Write the anchor to a file requests and Playwright can both read."""
    if pem is None:
        return None
    path = Path(directory) / "nebari-trust-anchor.pem"
    path.write_text(pem)
    return str(path)


def gateway_reachable(address: str, port: int = 443, timeout: float = 5) -> bool:
    """Plain TCP connect check: a liveness probe, not a request.

    Meant to run once per session, before anything else touches the
    gateway. The timeout is deliberately short: this only answers "is
    anything listening here", not "does TLS work" or "does the app
    respond", so it should fail fast rather than eat a connect timeout
    for every journey that would otherwise discover the same thing on
    its own.
    """
    try:
        with socket.create_connection((address, port), timeout=timeout):
            return True
    except OSError:
        return False


# TLS versions that count as actually encrypting the connection. Anything
# below 1.2 is deprecated (RFC 8996) and Python's default context will not
# negotiate it, so seeing one of these is the positive signal.
ACCEPTABLE_TLS_VERSIONS = frozenset({"TLSv1.2", "TLSv1.3"})


def negotiated_tls(
    domain: str,
    address: str,
    port: int = 443,
    ca_file: str | None = None,
    timeout: float = 10,
) -> str:
    """Complete a real TLS handshake and return the negotiated protocol.

    Verification is FULL and is never relaxed: the certificate must chain
    to `ca_file` (or to the public store when None), must be within its
    validity window, and must carry a SAN matching `domain` -- Python's
    default context checks the hostname.

    The point of taking `ca_file` is that "is this certificate valid" and
    "is this certificate publicly trusted" are different questions, and
    only the second one depends on who signed it. Pointed at the chain the
    cluster itself serves, this still catches an expired certificate, a
    certificate issued for the wrong name, a broken or incomplete chain,
    and a gateway not serving TLS at all -- on a Let's Encrypt STAGING or
    self-signed cluster, where asking about public trust could only ever
    return "no" and tell you nothing about the platform.
    """
    context = ssl.create_default_context(cafile=ca_file)
    with (
        socket.create_connection((address, port), timeout=timeout) as sock,
        context.wrap_socket(sock, server_hostname=domain) as tls,
    ):
        return tls.version()


def needs_dns_mapping(domain: str, gateway_address: str) -> bool:
    """True when public DNS does not already point the domain at the gateway."""
    try:
        infos = socket.getaddrinfo(domain, 443)
    except socket.gaierror:
        return True
    return not any(info[4][0] == gateway_address for info in infos)


def install_dns_mapping(
    domain: str,
    address: str,
    exempt_hosts: Iterable[str] = (),
) -> Callable[[], None]:
    """Map *.domain and domain itself to address for this process.

    Returns a callable that restores the original resolver.

    `exempt_hosts` are names that fall under `domain` but must keep
    resolving normally. This is not a nicety. The patch rebinds
    `socket.getaddrinfo`, which urllib3 calls as a module attribute
    (`socket.getaddrinfo(host, port, family, socket.SOCK_STREAM)` in
    urllib3/util/connection.py), so the Kubernetes client resolves through
    it too. On a cluster whose API server lives under the platform domain
    -- unusual on managed clouds, entirely plausible on-prem -- every
    Kubernetes API call issued after this fixture installs would be sent
    to the Envoy gateway instead of the API server, and the resulting
    failures would look like a broken cluster rather than a hijacked
    resolver. The caller passes the kubeconfig's API host (see
    `Cluster.api_host`).

    Comparison is case-insensitive on both sides: DNS names are, and a
    kubeconfig is free to spell the API host however it likes.
    """
    original = socket.getaddrinfo
    domain = domain.lower()
    suffix = f".{domain}"
    exempt = {host.lower() for host in exempt_hosts if host}

    def patched(host, port, *args, **kwargs):
        if isinstance(host, str):
            name = host.lower()
            if name not in exempt and (name == domain or name.endswith(suffix)):
                return original(address, port, *args, **kwargs)
        return original(host, port, *args, **kwargs)

    socket.getaddrinfo = patched

    def undo() -> None:
        socket.getaddrinfo = original

    return undo


def chromium_args(
    domain: str,
    address: str,
    mapping_needed: bool,
    trust_anchor_pem: str | None = None,
    anchor_trust: str = PUBLICLY_TRUSTED,
) -> list[str]:
    """Chromium flags mapping the domain and, where needed, pinning trust.

    --host-resolver-rules is an unsupported Chromium flag, used only when
    public DNS does not resolve. It maps names without weakening TLS: SNI
    and certificate validation still apply.

    Two rules, comma-separated in the one flag Chromium accepts: `MAP
    *.domain` does NOT match the bare apex, so without the second rule the
    apex would resolve differently in Chromium than in the Python client,
    where install_dns_mapping() explicitly handles `host == domain`. The two
    mapping paths must agree, or a journey that reaches the apex passes under
    requests and fails under Chromium for reasons that look like a platform
    fault.

    `trust_anchor_pem`, when given, is the same PEM `trust_anchor_pem()`
    (the module-level function in this file) derived from the cluster: a
    self-signed leaf that Chromium/NSS cannot accept as a CA (see the
    module docstring for why). This emits
    `--ignore-certificate-errors-spki-list=<hash>`, pinning Chromium's
    trust to that certificate's exact public key. When no anchor was
    derived -- the cluster's certificate chain is publicly trusted (ACME)
    -- nothing is emitted here, so an ACME cluster is driven by exactly
    the flags a real user's browser would see.
    """
    args = []
    if mapping_needed:
        args.append(
            f"--host-resolver-rules=MAP *.{domain} {address},MAP {domain} {address}"
        )
    # The pin is emitted ONLY for a chain the world does not trust.
    #
    # This used to key off `trust_anchor_pem` being non-None, which is a
    # different question and one that is almost always true:
    # `trust_anchor_pem()` falls back to `tls.crt`, so it returns a PEM on
    # every cert-manager cluster including production ACME. The result was
    # that Chromium ran with `--ignore-certificate-errors-spki-list` even
    # against a publicly trusted chain, so the browser journeys never
    # exercised real certificate validation on the one cluster shape where
    # they could have -- while this module's docstring claimed the exact
    # opposite. Verified against a live production-ACME cluster before the
    # fix: the flag was present.
    if anchor_trust != PUBLICLY_TRUSTED and trust_anchor_pem:
        args.append(
            f"--ignore-certificate-errors-spki-list={spki_sha256_b64(trust_anchor_pem)}"
        )
    return args
