import time

import pytest

from nebari_journeys.waits import (
    poll_until_settled,
    wait_for_condition,
    wait_for_value,
)


def test_returns_immediately_when_already_true():
    calls = []

    def check():
        calls.append(1)
        return True

    wait_for_condition(check, timeout=5, interval=0.01, description="instant")
    assert len(calls) == 1


def test_polls_until_true():
    state = {"n": 0}

    def check():
        state["n"] += 1
        return state["n"] >= 3

    wait_for_condition(check, timeout=5, interval=0.01, description="third try")
    assert state["n"] == 3


def test_raises_timeout_with_description():
    with pytest.raises(TimeoutError, match="pvc to bind"):
        wait_for_condition(
            lambda: False, timeout=0.05, interval=0.01, description="pvc to bind"
        )


def test_wait_for_value_returns_first_non_none():
    values = iter([None, None, "found"])
    got = wait_for_value(
        lambda: next(values), timeout=5, interval=0.01, description="value"
    )
    assert got == "found"


def test_wait_for_value_times_out():
    with pytest.raises(TimeoutError, match="never arrives"):
        wait_for_value(
            lambda: None, timeout=0.05, interval=0.01, description="never arrives"
        )


def test_timeout_is_enforced_not_merely_counted():
    start = time.monotonic()
    with pytest.raises(TimeoutError):
        wait_for_condition(
            lambda: False, timeout=0.1, interval=0.01, description="bounded"
        )
    assert time.monotonic() - start < 1.0


@pytest.mark.parametrize("falsy", [0, "", False, []])
def test_wait_for_value_returns_falsy_values_that_are_not_none(falsy):
    """A regression guard: `if value:` instead of `if value is not None:`
    would silently treat these as not-ready and time out."""
    got = wait_for_value(
        lambda: falsy, timeout=0.05, interval=0.01, description="falsy value"
    )
    assert got == falsy


# --- poll_until_settled ----------------------------------------------------


def test_poll_until_settled_returns_immediately_when_already_settled():
    calls = []

    def fetch():
        calls.append(1)
        return "healthy"

    assert (
        poll_until_settled(fetch, lambda v: v == "healthy", timeout=5, interval=0)
        == "healthy"
    )
    assert len(calls) == 1


def test_poll_until_settled_polls_until_the_value_converges():
    values = iter(["degraded", "degraded", "healthy"])
    assert (
        poll_until_settled(
            lambda: next(values), lambda v: v == "healthy", timeout=5, interval=0
        )
        == "healthy"
    )


def test_poll_until_settled_returns_the_last_value_on_timeout_without_raising():
    """The caller's own assertion carries the diagnosis, so a timeout must
    hand back the real unconverged state rather than a bare TimeoutError."""
    assert (
        poll_until_settled(
            lambda: "degraded", lambda v: v == "healthy", timeout=0, interval=0
        )
        == "degraded"
    )


def test_poll_until_settled_enforces_the_timeout_rather_than_counting_attempts():
    started = time.monotonic()
    poll_until_settled(lambda: "degraded", lambda v: False, timeout=0.05, interval=0.01)
    assert time.monotonic() - started >= 0.05
