import time

import pytest

from nebari_journeys.waits import wait_for_condition, wait_for_value


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
