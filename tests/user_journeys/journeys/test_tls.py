"""Journeys whose SUBJECT is certificate validity or chain trust itself.

Marked `tls` (see pytest.ini). These skip on a cluster whose gateway
anchor is a self-signed leaf (the shape cert-manager's default
`selfsigned-issuer` produces, see nebari_journeys.trust), because a
CA:FALSE leaf can never pass a real chain-validation check by design;
that is a cluster configuration gap tracked as issue #447, not a bug in
the test. On a cluster with a properly issued (for example ACME) chain,
this is expected to pass with no cluster-derived anchor and no relaxed
verification at all: it exists to prove the platform still gets a real
chain when one is configured, since every other journey in this suite
either supplies the cluster's own anchor to `requests` or pins Chromium
to it via SPKI, and neither of those would notice the chain silently
regressing to something a real, unmodified browser could not validate.
"""

import pytest
import requests

pytestmark = pytest.mark.tls


def test_gateway_certificate_validates_against_the_system_trust_store(
    platform_domain, dns_mapping, gateway_reachable
):
    """No `verify=<cluster anchor>`, no `ignore_https_errors`: plain
    `verify=True`, the same default a real client gets. Any TLS failure
    here means the gateway's certificate is not usable by a client that
    has not been specially configured to trust this cluster, which is a
    real platform problem."""
    try:
        requests.get(f"https://{platform_domain}/", timeout=10, verify=True)
    except requests.exceptions.SSLError as exc:
        pytest.fail(
            f"gateway certificate for {platform_domain!r} does not validate "
            f"against the system trust store with no cluster-supplied anchor: "
            f"{exc}"
        )
