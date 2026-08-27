"""Cluster discovery.

A kubeconfig is the only required input. Everything else - domain,
gateway address, admin credentials, which optional components exist -
is read from the cluster, so the suite runs against a cluster this
checkout never deployed.
"""

import base64
from dataclasses import dataclass

import pytest
from kubernetes import client
from kubernetes import config as kubeconfig

from nebari_journeys import constants
from nebari_journeys.waits import wait_for_value

ARGOCD_GROUP = "argoproj.io"
ARGOCD_VERSION = "v1alpha1"
ARGOCD_PLURAL = "applications"

CERTMANAGER_GROUP = "cert-manager.io"
CERTMANAGER_VERSION = "v1"
CERTIFICATE_PLURAL = "certificates"


@dataclass
class Cluster:
    """Entry point for every cluster action a journey performs."""

    core: client.CoreV1Api
    custom: client.CustomObjectsApi

    @classmethod
    def connect(cls) -> "Cluster":
        try:
            kubeconfig.load_kube_config()
        except kubeconfig.ConfigException:
            kubeconfig.load_incluster_config()
        return cls(core=client.CoreV1Api(), custom=client.CustomObjectsApi())

    def secret_value(self, namespace: str, name: str, key: str) -> str:
        secret = self.core.read_namespaced_secret(name=name, namespace=namespace)
        data = secret.data or {}
        if key not in data:
            raise KeyError(
                f"key {key!r} not in secret {namespace}/{name}; "
                f"present keys: {sorted(data)}"
            )
        return base64.b64decode(data[key]).decode()

    def keycloak_admin_password(self) -> str:
        return self.secret_value(
            constants.KEYCLOAK_NAMESPACE,
            constants.KEYCLOAK_ADMIN_SECRET,
            constants.KEYCLOAK_ADMIN_PASSWORD_KEY,
        )

    def realm_admin_password(self) -> str:
        return self.secret_value(
            constants.KEYCLOAK_NAMESPACE,
            constants.REALM_ADMIN_SECRET,
            constants.REALM_ADMIN_PASSWORD_KEY,
        )

    def gateway_address(self) -> str:
        """IP if the load balancer has one, else its hostname."""

        def fetch():
            services = self.core.list_namespaced_service(
                namespace=constants.GATEWAY_NAMESPACE,
                label_selector=constants.GATEWAY_LABEL_SELECTOR,
            )
            for svc in services.items:
                ingress = (svc.status.load_balancer.ingress or [None])[0]
                if ingress is None:
                    continue
                if ingress.ip:
                    return ingress.ip
                if ingress.hostname:
                    return ingress.hostname
            return None

        return wait_for_value(fetch, description="the gateway load balancer address")

    def domain(self) -> str:
        """Platform domain, from the gateway Certificate's commonName."""
        cert = self.custom.get_namespaced_custom_object(
            group=CERTMANAGER_GROUP,
            version=CERTMANAGER_VERSION,
            namespace=constants.GATEWAY_NAMESPACE,
            plural=CERTIFICATE_PLURAL,
            name=constants.GATEWAY_CERTIFICATE_NAME,
        )
        common_name = cert.get("spec", {}).get("commonName")
        if not common_name:
            raise ValueError(
                f"{constants.GATEWAY_CERTIFICATE_NAME} has no spec.commonName; "
                "cannot determine the platform domain"
            )
        return common_name

    def applications(self) -> list[dict]:
        result = self.custom.list_namespaced_custom_object(
            group=ARGOCD_GROUP,
            version=ARGOCD_VERSION,
            namespace=constants.ARGOCD_NAMESPACE,
            plural=ARGOCD_PLURAL,
        )
        return result.get("items", [])

    def has_app(self, name: str) -> bool:
        return any(a["metadata"]["name"] == name for a in self.applications())

    def require_app(self, name: str) -> None:
        """Skip the calling test when an optional component is not installed."""
        if not self.has_app(name):
            pytest.skip(f"ArgoCD application {name!r} is not deployed on this cluster")
