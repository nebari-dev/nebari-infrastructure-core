"""Polling helpers.

Journeys wait on a real cluster, so every wait is bounded and every
timeout says what it was waiting for. A bare TimeoutError in CI is
almost useless; the description is the whole point.
"""

import time
from collections.abc import Callable

DEFAULT_TIMEOUT = 120.0
DEFAULT_INTERVAL = 2.0


def wait_for_condition(
    check: Callable[[], bool],
    *,
    timeout: float = DEFAULT_TIMEOUT,
    interval: float = DEFAULT_INTERVAL,
    description: str = "condition",
) -> None:
    """Poll check() until it returns True, or raise TimeoutError."""
    deadline = time.monotonic() + timeout
    while True:
        if check():
            return
        if time.monotonic() >= deadline:
            raise TimeoutError(
                f"timed out after {timeout:g}s waiting for {description}"
            )
        time.sleep(interval)


def wait_for_value[T](
    fetch: Callable[[], T | None],
    *,
    timeout: float = DEFAULT_TIMEOUT,
    interval: float = DEFAULT_INTERVAL,
    description: str = "value",
) -> T:
    """Poll fetch() until it returns a non-None value, or raise TimeoutError."""
    deadline = time.monotonic() + timeout
    while True:
        value = fetch()
        if value is not None:
            return value
        if time.monotonic() >= deadline:
            raise TimeoutError(
                f"timed out after {timeout:g}s waiting for {description}"
            )
        time.sleep(interval)


def poll_until_settled[T](
    fetch: Callable[[], T],
    is_settled: Callable[[T], bool],
    *,
    timeout: float = DEFAULT_TIMEOUT,
    interval: float = DEFAULT_INTERVAL,
) -> T:
    """Poll fetch() until is_settled(), returning the LAST value either way.

    The other two waits raise on timeout, which is right when there is
    nothing useful to say about a value that never arrived. This one is for
    the opposite case: a value that is always present but takes time to
    converge -- ArgoCD health right after a deploy, Longhorn robustness
    right after an attach. The caller asserts on the result and already has
    a precise message for the unconverged state, so a bare TimeoutError
    here would replace a real diagnosis with a worse one.

    Callers must therefore treat a non-settled return as a real result and
    say something about it, not assume settlement.
    """
    deadline = time.monotonic() + timeout
    while True:
        value = fetch()
        if is_settled(value) or time.monotonic() >= deadline:
            return value
        time.sleep(interval)
