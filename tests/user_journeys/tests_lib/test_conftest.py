"""Unit tests for the journeys/ session fixtures.

journeys/ needs a live cluster to collect through pytest normally, but the
fixture FUNCTIONS themselves are plain Python and can be exercised directly
here with the cluster-dependent fixture values (platform_domain,
gateway_address) faked, and nebari_journeys.trust mocked.
"""

from unittest.mock import patch

import pytest

from journeys.conftest import gateway_reachable as gateway_reachable_fixture

# Fixture functions refuse to be called directly (pytest raises on it), so
# the underlying function is exercised through __wrapped__: it is a plain
# Python function like any other action, and this is the only way to unit
# test its message without spinning up a real cluster and session fixtures.
gateway_reachable = gateway_reachable_fixture.__wrapped__


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
