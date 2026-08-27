"""Trust anchor derivation and name resolution.

TLS verification is never disabled in this suite. Where the platform
domain does not resolve to the gateway, names are mapped for both the
Python client and Chromium, which preserves real SNI and real
certificate validation rather than bypassing it.
"""

import socket
from collections.abc import Callable
from pathlib import Path

from kubernetes.client.rest import ApiException

from nebari_journeys import constants

CA_KEY = "ca.crt"
LEAF_KEY = "tls.crt"

NOT_FOUND_STATUS = 404


def trust_anchor_pem(cluster) -> str | None:
    """PEM to verify the gateway against, or None to use the system store.

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
    for key in (CA_KEY, LEAF_KEY):
        try:
            pem = cluster.secret_value(
                constants.GATEWAY_NAMESPACE, constants.GATEWAY_TLS_SECRET, key
            )
        except KeyError:
            continue
        except ApiException as error:
            if error.status == NOT_FOUND_STATUS:
                return None
            raise RuntimeError(
                f"could not read {key!r} from secret "
                f"{constants.GATEWAY_NAMESPACE}/{constants.GATEWAY_TLS_SECRET}: "
                f"{error}"
            ) from error
        if pem and pem.strip():
            return pem
    return None


def write_trust_anchor(pem: str | None, directory) -> str | None:
    """Write the anchor to a file requests and Playwright can both read."""
    if pem is None:
        return None
    path = Path(directory) / "nebari-trust-anchor.pem"
    path.write_text(pem)
    return str(path)


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


def chromium_args(domain: str, address: str, mapping_needed: bool) -> list[str]:
    """Chromium flags mapping the domain, or nothing when DNS already works.

    --host-resolver-rules is an unsupported Chromium flag, used only when
    public DNS does not resolve. It maps names without weakening TLS: SNI
    and certificate validation still apply.
    """
    if not mapping_needed:
        return []
    return [f"--host-resolver-rules=MAP *.{domain} {address}"]
