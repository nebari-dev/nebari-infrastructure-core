"""The shape a sweep of leftovers reports back.

Lives in its own module so both `k8s` (namespaces) and `keycloak`
(scratch realm users) can describe their sweeps the same way without
either importing the other: `keycloak` must stay free of the Kubernetes
client, and `k8s` must stay free of `requests`.
"""

from dataclasses import dataclass, field


@dataclass
class SweepResult:
    """What the sweep did. No logging in library code; the caller reports.

    `deleted` is what the sweep removed. `skipped` is what carried the
    journey marker but failed the second guard, which is an anomaly the
    caller must surface rather than swallow. `failed` is what the sweep
    tried and could not remove, which is the case that actually leaves
    something behind.
    """

    deleted: list[str] = field(default_factory=list)
    skipped: list[str] = field(default_factory=list)
    failed: list[str] = field(default_factory=list)
