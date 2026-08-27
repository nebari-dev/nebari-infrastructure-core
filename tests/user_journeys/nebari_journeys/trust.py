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

This is strictly worse than driving a real CA-issued (or ACME) chain
through unmodified Chromium, which is why an ACME cluster gets no
extra flags at all (see `chromium_args`). The real fix is issue #447:
once cert-manager issues a proper CA for the gateway, Chromium can
validate the chain like any other browser, and this SPKI pin -- along
with `spki_sha256_b64` and the flag it feeds -- should be removed.
"""

import base64
import hashlib
import socket
from collections.abc import Callable
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
from kubernetes.client.rest import ApiException

CA_KEY = "ca.crt"
LEAF_KEY = "tls.crt"

NOT_FOUND_STATUS = 404


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


def needs_dns_mapping(domain: str, gateway_address: str) -> bool:
    """True when public DNS does not already point the domain at the gateway."""
    try:
        infos = socket.getaddrinfo(domain, 443)
    except socket.gaierror:
        return True
    return not any(info[4][0] == gateway_address for info in infos)


def install_dns_mapping(domain: str, address: str) -> Callable[[], None]:
    """Map *.domain and domain itself to address for this process.

    Returns a callable that restores the original resolver.
    """
    original = socket.getaddrinfo
    suffix = f".{domain}"

    def patched(host, port, *args, **kwargs):
        if isinstance(host, str) and (host == domain or host.endswith(suffix)):
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
    if trust_anchor_pem:
        args.append(
            f"--ignore-certificate-errors-spki-list={spki_sha256_b64(trust_anchor_pem)}"
        )
    return args
