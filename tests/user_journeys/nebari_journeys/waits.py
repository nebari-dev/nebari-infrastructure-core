"""Polling helpers.

Journeys wait on a real cluster, so every wait is bounded and every
timeout says what it was waiting for. A bare TimeoutError in CI is
almost useless; the description is the whole point.
"""

import time
from typing import Callable, TypeVar

T = TypeVar("T")

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


def wait_for_value(
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
