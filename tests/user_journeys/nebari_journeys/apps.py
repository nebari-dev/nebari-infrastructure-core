"""NebariApps, and the sign-in the nebari-operator wires up for them.

Packs register their UIs as NebariApps. For each one with auth enabled the
operator provisions a Keycloak client and, unless the app opts out with
`auth.enforceAtGateway: false`, an Envoy Gateway SecurityPolicy that makes
the gateway itself demand a Keycloak login before any request reaches the
app. These are the verbs for finding those apps and reading what the
operator built for them.

Read-only: journeys check the NebariApps the cluster's packs declared and
never create one. Verbs only; the assertions live in the journeys.
"""

import time
from dataclasses import dataclass
from datetime import UTC, datetime
from urllib.parse import urlparse

import requests

from nebari_journeys import constants

# The nebari-operator's CRD (api/v1/nebariapp_types.go there).
NEBARIAPP_GROUP = "reconcilers.nebari.dev"
NEBARIAPP_VERSION = "v1"
NEBARIAPP_PLURAL = "nebariapps"
NEBARIAPP_KIND = "NebariApp"

# Envoy Gateway's CRD.
SECURITYPOLICY_GROUP = "gateway.envoyproxy.io"
SECURITYPOLICY_VERSION = "v1alpha1"
SECURITYPOLICY_PLURAL = "securitypolicies"

OIDC_DISCOVERY_PATH = "/.well-known/openid-configuration"
REQUEST_TIMEOUT = 30

# How long a journey waits for the operator to finish wiring up an app that
# a pack has only just declared.
APP_READY_TIMEOUT = 300.0
APP_READY_INTERVAL = 5.0


@dataclass(frozen=True)
class NebariApp:
    namespace: str
    name: str
    hostname: str
    auth_enabled: bool
    enforce_at_gateway: bool
    conditions: dict

    @classmethod
    def from_object(cls, obj: dict) -> "NebariApp":
        spec = obj.get("spec") or {}
        auth = spec.get("auth") or {}
        status = obj.get("status") or {}
        return cls(
            namespace=obj["metadata"]["namespace"],
            name=obj["metadata"]["name"],
            hostname=(spec.get("hostname") or "").lower(),
            auth_enabled=bool(auth.get("enabled")),
            enforce_at_gateway=gateway_enforced(auth),
            conditions={c["type"]: c for c in status.get("conditions") or []},
        )

    @property
    def ref(self) -> str:
        return f"{self.namespace}/{self.name}"

    def condition_status(self, condition_type: str) -> str | None:
        condition = self.conditions.get(condition_type)
        return condition.get("status") if condition else None


def gateway_enforced(auth: dict | None) -> bool:
    """Whether the operator puts this app's login in front of the gateway.

    Mirrors the operator's default: auth must be enabled, and an absent
    `enforceAtGateway` means true. Only an explicit `false` hands sign-in to
    the app itself (JupyterHub, for one, runs its own Keycloak login).
    """
    if not auth or not auth.get("enabled"):
        return False
    return auth.get("enforceAtGateway") is not False


def nebari_apps(cluster) -> list[NebariApp]:
    result = cluster.custom.list_cluster_custom_object(
        group=NEBARIAPP_GROUP,
        version=NEBARIAPP_VERSION,
        plural=NEBARIAPP_PLURAL,
    )
    return [NebariApp.from_object(o) for o in result.get("items", [])]


def nebari_apps_in(cluster, namespace: str) -> list[NebariApp]:
    return [a for a in nebari_apps(cluster) if a.namespace == namespace]


def controlled_by(obj: dict, kind: str, name: str) -> bool:
    """Whether `obj` carries a controller ownerReference to kind/name.

    The operator sets itself as controller of every SecurityPolicy it
    creates, so this finds an app's policy without depending on the
    operator's naming scheme.
    """
    return any(
        ref.get("kind") == kind and ref.get("name") == name and ref.get("controller")
        for ref in obj["metadata"].get("ownerReferences") or []
    )


def security_policies_for(cluster, app: NebariApp) -> list[dict]:
    result = cluster.custom.list_namespaced_custom_object(
        group=SECURITYPOLICY_GROUP,
        version=SECURITYPOLICY_VERSION,
        namespace=app.namespace,
        plural=SECURITYPOLICY_PLURAL,
    )
    return [
        p for p in result.get("items", []) if controlled_by(p, NEBARIAPP_KIND, app.name)
    ]


def oidc_provider(policy: dict) -> dict:
    return ((policy.get("spec") or {}).get("oidc") or {}).get("provider") or {}


def acceptance(policy: dict) -> list[dict]:
    """The `Accepted` condition from every ancestor (Gateway) the policy
    attaches to. Envoy Gateway reports acceptance per ancestor, so a policy
    is only fully accepted when every entry says so."""
    found = []
    for ancestor in (policy.get("status") or {}).get("ancestors") or []:
        for condition in ancestor.get("conditions") or []:
            if condition.get("type") == "Accepted":
                found.append(condition)
    return found


def wait_for_app_ready(cluster, app: NebariApp) -> NebariApp:
    """Re-read `app` until the operator reports it Ready, or give up and
    return the last state seen. Lets a journey run right after a pack is
    installed without racing the operator; the caller asserts on the
    result."""
    deadline = time.monotonic() + APP_READY_TIMEOUT
    current = app
    while True:
        fresh = [
            a for a in nebari_apps_in(cluster, app.namespace) if a.name == app.name
        ]
        if fresh:
            current = fresh[0]
        if current.condition_status("Ready") == "True" or time.monotonic() >= deadline:
            return current
        time.sleep(APP_READY_INTERVAL)


def advertised_issuer(platform_domain: str, verify: str | bool) -> str:
    """The issuer Keycloak's public discovery document advertises for the
    platform realm: the value every gateway-enforced app has to trust."""
    url = (
        f"https://keycloak.{platform_domain}/realms/{constants.REALM_NAME}"
        f"{OIDC_DISCOVERY_PATH}"
    )
    response = requests.get(url, verify=verify, timeout=REQUEST_TIMEOUT)
    response.raise_for_status()
    return response.json()["issuer"]


def anonymous_visit(url: str, verify: str | bool) -> requests.Response:
    """GET `url` as a visitor with no session, without following redirects,
    so the journey sees exactly where the gateway sends them."""
    return requests.get(
        url, allow_redirects=False, verify=verify, timeout=REQUEST_TIMEOUT
    )


def host_of(url: str) -> str:
    return (urlparse(url).hostname or "").lower()


def destination_namespace(cluster, app_name: str) -> str | None:
    """Where an ArgoCD Application installs its resources."""
    for app in cluster.applications():
        if app["metadata"]["name"] == app_name:
            return ((app.get("spec") or {}).get("destination") or {}).get("namespace")
    return None


def pod_logs_since(
    cluster, namespace: str, label_selector: str, since: datetime
) -> str:
    """Concatenated logs of every pod matching `label_selector`, from
    `since` onward. Used to read what a component did on a journey's
    behalf when the outcome itself is not observable from outside."""
    seconds = max(1, int((datetime.now(UTC) - since).total_seconds()) + 1)
    pods = cluster.core.list_namespaced_pod(namespace, label_selector=label_selector)
    return "\n".join(
        cluster.core.read_namespaced_pod_log(
            p.metadata.name, namespace, since_seconds=seconds
        )
        for p in pods.items
    )
