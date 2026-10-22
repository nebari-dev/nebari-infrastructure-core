"""Restarting platform pods, for the `disruptive` journeys only.

Every other journey stays inside its scratch namespace. These deliberately
do not: they check that sign-in survives the operator, Keycloak or the
Envoy proxy being replaced, which can only be done by replacing them. They
run only with --allow-disruption (see journeys/conftest.py).
"""

import time

RESTART_TIMEOUT = 600.0
RESTART_INTERVAL = 5.0

OPERATOR_NAMESPACE = "nebari-operator-system"
OPERATOR_SELECTOR = "control-plane=controller-manager"
KEYCLOAK_NAMESPACE = "keycloak"
KEYCLOAK_SELECTOR = "app.kubernetes.io/name=keycloakx"
ENVOY_NAMESPACE = "envoy-gateway-system"
ENVOY_PROXY_SELECTOR = "app.kubernetes.io/component=proxy"


def _ready(pod) -> bool:
    if pod.metadata.deletion_timestamp is not None:
        return False
    for condition in pod.status.conditions or []:
        if condition.type == "Ready":
            return condition.status == "True"
    return False


def restart_pods(cluster, namespace: str, selector: str) -> list[str]:
    """Delete every pod matching `selector` and wait until replacements
    (pods with different UIDs) are all Ready. Returns the replacements'
    names. Raises TimeoutError if they never become Ready: a platform
    component that cannot come back is itself the failure."""
    before = cluster.core.list_namespaced_pod(namespace, label_selector=selector).items
    if not before:
        raise RuntimeError(f"no pods match {selector!r} in {namespace}")
    old = {p.metadata.uid for p in before}
    for pod in before:
        cluster.core.delete_namespaced_pod(pod.metadata.name, namespace)

    deadline = time.monotonic() + RESTART_TIMEOUT
    while time.monotonic() < deadline:
        pods = cluster.core.list_namespaced_pod(
            namespace, label_selector=selector
        ).items
        fresh = [p for p in pods if p.metadata.uid not in old]
        if (
            len(fresh) >= len(before)
            and all(_ready(p) for p in fresh)
            and not [p for p in pods if p.metadata.uid in old]
        ):
            return [p.metadata.name for p in fresh]
        time.sleep(RESTART_INTERVAL)
    raise TimeoutError(
        f"pods matching {selector!r} in {namespace} were not replaced and Ready "
        f"within {RESTART_TIMEOUT:.0f}s"
    )
