"""TLS journeys.

Two separate questions, deliberately not merged:

1. Does the gateway serve a VALID certificate for this domain over a real
   encrypted connection? True or false on every cluster shape, and worth
   knowing on all of them.
2. Is that certificate PUBLICLY TRUSTED, so an ordinary client with no
   special configuration accepts it? Only answerable where the issuer is
   a public CA.

This file used to ask only the second question, and skipped entirely when
the answer could not be "yes". That threw away the first one -- which is
the one that catches an expired certificate, a certificate issued for the
wrong name, a broken chain, or a gateway not serving TLS at all. Every
cloud fixture in .github/fixtures/deploy/ uses the Let's Encrypt STAGING
endpoint on purpose, so on CI's own clusters the whole file went dark.
"""

import pytest
import requests

from nebari_journeys import trust


def test_gateway_serves_a_valid_certificate_for_this_domain(
    platform_domain, gateway_address, trust_anchor, dns_mapping, gateway_reachable
):
    """Runs on EVERY cluster shape, including self-signed and staging.

    Verification is full, never relaxed: the handshake below checks the
    chain, the validity window, and the hostname against the SANs. What
    varies by cluster is only WHICH anchor the chain has to reach --
    `trust_anchor` is None on a publicly trusted cluster (so the public
    store is used, exactly as a real client would) and otherwise the chain
    the cluster itself serves.

    So a staging cluster still proves: TLS is actually negotiated, the
    connection is encrypted at TLS 1.2 or better, the certificate has not
    expired, it was issued for this domain, and the chain is complete.
    The only thing it cannot prove is that a stranger would trust it,
    which is what the journey below is for.
    """
    version = trust.negotiated_tls(
        platform_domain, gateway_address, ca_file=trust_anchor
    )
    assert version in trust.ACCEPTABLE_TLS_VERSIONS, (
        f"gateway for {platform_domain!r} negotiated {version!r}, which is not "
        f"one of {sorted(trust.ACCEPTABLE_TLS_VERSIONS)}; the connection is not "
        "protected by a currently acceptable TLS version"
    )


@pytest.mark.tls
def test_gateway_certificate_is_publicly_trusted(
    platform_domain, dns_mapping, gateway_reachable
):
    """No `verify=<cluster anchor>`, no `ignore_https_errors`: plain
    `verify=True`, the same default a real client gets. Any TLS failure
    here means the gateway's certificate is not usable by a client that
    has not been specially configured to trust this cluster.

    Marked `tls`, so it skips where public trust is not on offer: a
    self-signed leaf (#447) or a privately issued chain such as Let's
    Encrypt staging. Those are not platform defects, and the journey above
    still runs there.
    """
    try:
        requests.get(f"https://{platform_domain}/", timeout=10, verify=True)
    except requests.exceptions.SSLError as exc:
        pytest.fail(
            f"gateway certificate for {platform_domain!r} does not validate "
            f"against the system trust store with no cluster-supplied anchor: "
            f"{exc}"
        )
