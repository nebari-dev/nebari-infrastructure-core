"""Unit tests for the journeys/ session fixtures.

journeys/ needs a live cluster to collect through pytest normally, but the
fixture FUNCTIONS themselves are plain Python and can be exercised directly
here with the cluster-dependent fixture values (platform_domain,
gateway_address) faked, and nebari_journeys.trust mocked.
"""

from unittest.mock import MagicMock, patch

import pytest

import journeys.conftest as _journeys_conftest
from journeys.conftest import gateway_reachable as gateway_reachable_fixture
from tests_lib.test_trust import SELF_SIGNED_CA_PEM, SELF_SIGNED_LEAF_PEM

# Fixture functions refuse to be called directly (pytest raises on it), so
# the underlying function is exercised through __wrapped__: it is a plain
# Python function like any other action, and this is the only way to unit
# test its message without spinning up a real cluster and session fixtures.
#
# Accessed off the module object (`_journeys_conftest.x`), NOT via a bare
# `from journeys.conftest import x` binding: pytest's fixture discovery
# registers a fixture-decorated object under its ORIGINAL name wherever it
# is bound as a module-level global, regardless of the local alias. Binding
# `self_signed_anchor` (a dependency) and the AUTOUSE
# `skip_trusted_ca_marked_tests_on_self_signed` fixture that way made pytest
# apply the latter to every test in this file and fail resolving
# `self_signed_anchor` back to a real `cluster` fixture that does not exist
# here. Going through the module object avoids exposing the raw
# fixture-marked objects as globals at all.
gateway_reachable = gateway_reachable_fixture.__wrapped__
self_signed_anchor = _journeys_conftest.self_signed_anchor.__wrapped__
skip_trusted_ca_marked_tests_on_self_signed = (
    _journeys_conftest.skip_trusted_ca_marked_tests_on_self_signed.__wrapped__
)
SELF_SIGNED_TRUSTED_CA_WARNING = _journeys_conftest.SELF_SIGNED_TRUSTED_CA_WARNING


def test_gateway_reachable_returns_true_when_the_gateway_is_up():
    with patch("nebari_journeys.trust.gateway_reachable", return_value=True):
        assert gateway_reachable("nebari.example", "10.0.0.5") is True


def test_gateway_reachable_fails_with_a_diagnosis_naming_domain_and_address():
    with (
        patch("nebari_journeys.trust.gateway_reachable", return_value=False),
        pytest.raises(pytest.fail.Exception) as excinfo,
    ):
        gateway_reachable("nebari.example", "192.168.1.100")

    message = str(excinfo.value)
    assert "nebari.example" in message
    assert "192.168.1.100" in message
    assert "443" in message
    assert "612" in message


def _request_marked_requires_trusted_ca(marked: bool):
    """A fake pytest `request` whose closest marker lookup behaves like a
    real one for the one marker this fixture cares about."""
    request = MagicMock()
    request.node.get_closest_marker.side_effect = lambda name: (
        MagicMock() if marked and name == "requires_trusted_ca" else None
    )
    return request


def test_self_signed_anchor_is_true_for_a_self_signed_leaf():
    """The cert-manager selfsigned-issuer shape: CA:FALSE, subject == issuer."""
    assert self_signed_anchor(SELF_SIGNED_LEAF_PEM) is True


def test_self_signed_anchor_is_false_for_a_ca_issued_certificate():
    """A real issuing CA (even a self-signed root, CA:TRUE) is not this shape."""
    assert self_signed_anchor(SELF_SIGNED_CA_PEM) is False


def test_self_signed_anchor_is_false_when_no_anchor_was_derived():
    """`trust_anchor_pem` returning None means a publicly trusted (ACME)
    chain: nothing to flag as self-signed."""
    assert self_signed_anchor(None) is False


def test_requires_trusted_ca_marked_journey_skips_on_a_self_signed_leaf():
    """The one behavior PR #642 exists to add: a journey marked
    `requires_trusted_ca` must SKIP, not fail, when the gateway anchor is a
    self-signed leaf, since ArgoCD (a third party, not the test runner)
    cannot trust that certificate for server-side OIDC discovery."""
    request = _request_marked_requires_trusted_ca(marked=True)
    with pytest.raises(pytest.skip.Exception) as excinfo:
        skip_trusted_ca_marked_tests_on_self_signed(request, self_signed_anchor=True)
    assert str(excinfo.value) == SELF_SIGNED_TRUSTED_CA_WARNING


def test_skip_reason_names_the_mechanism_and_every_issue():
    """Asserted separately from the previous test so a future edit that
    quietly waters the message down to something uninformative is caught
    even if the skip itself still fires correctly."""
    assert "ArgoCD" in SELF_SIGNED_TRUSTED_CA_WARNING
    assert "UNVERIFIED" in SELF_SIGNED_TRUSTED_CA_WARNING
    assert "known broken" in SELF_SIGNED_TRUSTED_CA_WARNING
    assert "server-side OIDC discovery" in SELF_SIGNED_TRUSTED_CA_WARNING
    assert "#490" in SELF_SIGNED_TRUSTED_CA_WARNING
    assert "#447" in SELF_SIGNED_TRUSTED_CA_WARNING
    assert "#607" in SELF_SIGNED_TRUSTED_CA_WARNING


def test_requires_trusted_ca_marked_journey_does_not_skip_on_a_ca_issued_anchor():
    """On a cluster with a real issuing CA, this journey must run (and can
    still fail if SSO is actually broken there): the skip is scoped to the
    self-signed-leaf shape, not to the symptom."""
    request = _request_marked_requires_trusted_ca(marked=True)
    # No exception raised means no skip.
    skip_trusted_ca_marked_tests_on_self_signed(request, self_signed_anchor=False)


def test_requires_trusted_ca_marked_journey_does_not_skip_with_no_anchor_at_all():
    """A publicly trusted (ACME) cluster derives no anchor at all
    (`trust_anchor_pem` returns None, so `self_signed_anchor` is False);
    this journey must still run there too."""
    request = _request_marked_requires_trusted_ca(marked=True)
    skip_trusted_ca_marked_tests_on_self_signed(request, self_signed_anchor=False)


def test_unmarked_journey_never_skips_even_on_a_self_signed_leaf():
    """Scope check: only the one marked journey is affected. A journey
    without `requires_trusted_ca` must keep running on a self-signed
    cluster, whatever `self_signed_anchor` says."""
    request = _request_marked_requires_trusted_ca(marked=False)
    skip_trusted_ca_marked_tests_on_self_signed(request, self_signed_anchor=True)
