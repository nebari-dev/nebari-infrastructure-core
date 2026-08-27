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
from kubernetes.client.rest import ApiException

from nebari_journeys import constants
from nebari_journeys.waits import wait_for_value

ARGOCD_GROUP = "argoproj.io"
ARGOCD_VERSION = "v1alpha1"
ARGOCD_PLURAL = "applications"

CERTMANAGER_GROUP = "cert-manager.io"
CERTMANAGER_VERSION = "v1"
CERTIFICATE_PLURAL = "certificates"

GATEWAY_API_GROUP = "gateway.networking.k8s.io"
GATEWAY_API_VERSION = "v1"
GATEWAY_PLURAL = "gateways"
HTTPROUTE_PLURAL = "httproutes"

STORAGE_DEFAULT_ANNOTATION = "storageclass.kubernetes.io/is-default-class"
LONGHORN_GROUP = "longhorn.io"
LONGHORN_VERSION = "v1beta2"
VOLUME_PLURAL = "volumes"

# Longhorn's own default StorageClass name, created by the Longhorn chart
# itself rather than by NIC, which is why it is not a mirrored constant and
# is not pinned by the Go contract test.
LONGHORN_STORAGE_CLASS = "longhorn"
FALLBACK_STORAGE_CLASS = LONGHORN_STORAGE_CLASS

NOT_FOUND_STATUS = 404


@dataclass
class Cluster:
    """Entry point for every cluster action a journey performs."""

    core: client.CoreV1Api
    custom: client.CustomObjectsApi
    storage: client.StorageV1Api = None

    @classmethod
    def connect(cls) -> "Cluster":
        try:
            kubeconfig.load_kube_config()
        except kubeconfig.ConfigException as kubeconfig_error:
            # Running inside the cluster is legitimate, so fall back. But if
            # that fails too, the kubeconfig problem is almost always the
            # real cause, and reporting only the in-cluster failure hides it.
            try:
                kubeconfig.load_incluster_config()
            except kubeconfig.ConfigException as incluster_error:
                raise kubeconfig.ConfigException(
                    f"could not load a kubeconfig ({kubeconfig_error}) and no "
                    f"in-cluster config is available ({incluster_error})"
                ) from kubeconfig_error
        return cls(
            core=client.CoreV1Api(),
            custom=client.CustomObjectsApi(),
            storage=client.StorageV1Api(),
        )

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

    def gateway(self) -> dict | None:
        """The Nebari Gateway object, or None when it does not exist."""
        try:
            return self.custom.get_namespaced_custom_object(
                group=GATEWAY_API_GROUP,
                version=GATEWAY_API_VERSION,
                namespace=constants.GATEWAY_NAMESPACE,
                plural=GATEWAY_PLURAL,
                name=constants.GATEWAY_NAME,
            )
        except ApiException as error:
            if error.status == NOT_FOUND_STATUS:
                return None
            raise

    def gateway_tls_secret_ref(self) -> tuple[str, str]:
        """(name, namespace) of the TLS secret the gateway actually serves.

        Both are operator-configurable (pkg/config/config.go,
        CertificateConfig.GatewaySecretRef): `certificate.secret_name`
        renames it, and `certificate.existing_secret` can put it in another
        namespace entirely. Assuming the default name would silently degrade
        the trust anchor to the system store on a supported cluster shape, so
        the reference is read from the Gateway's own
        `listeners[].tls.certificateRefs` instead. The default is only used
        when the Gateway cannot be read at all, so a cluster without the
        Gateway API still behaves as before.
        """
        gateway = self.gateway()
        if gateway is None:
            return constants.GATEWAY_TLS_SECRET, constants.GATEWAY_NAMESPACE

        listeners = gateway.get("spec", {}).get("listeners") or []
        for listener in listeners:
            refs = (listener.get("tls") or {}).get("certificateRefs") or []
            for ref in refs:
                name = ref.get("name")
                if not name:
                    continue
                # A certificateRef without an explicit namespace resolves in
                # the Gateway's own namespace, per the Gateway API spec.
                return name, ref.get("namespace") or constants.GATEWAY_NAMESPACE

        return constants.GATEWAY_TLS_SECRET, constants.GATEWAY_NAMESPACE

    def gateway_route_hostnames(self) -> list[str]:
        """Every hostname served by an HTTPRoute attached to the gateway.

        An empty list when the HTTPRoute CRD is not installed at all, so the
        caller can fall back and then report what it looked for, rather than
        surfacing a raw 404 from the API server.
        """
        try:
            routes = self.custom.list_cluster_custom_object(
                group=GATEWAY_API_GROUP,
                version=GATEWAY_API_VERSION,
                plural=HTTPROUTE_PLURAL,
            ).get("items", [])
        except ApiException as error:
            if error.status != NOT_FOUND_STATUS:
                raise
            return []

        hostnames: list[str] = []
        for route in routes:
            spec = route.get("spec") or {}
            parents = spec.get("parentRefs") or []
            if not any(
                parent.get("name") == constants.GATEWAY_NAME for parent in parents
            ):
                continue
            hostnames.extend(spec.get("hostnames") or [])
        return hostnames

    def domain(self) -> str:
        """The platform domain, discovered from what the gateway serves.

        HTTPRoutes are the primary source because they exist on every
        supported certificate shape. The gateway Certificate does not: with
        `certificate.type: existing` NIC never renders
        gateway-certificate.yaml (pkg/argocd/writer.go,
        skipCertificateTemplate), so reading the Certificate first made every
        journey error at session setup on that shape, including journeys with
        nothing to do with certificates.

        Nebari's own HTTPRoutes are all `<service>.<domain>` (argocd,
        keycloak, longhorn), so the domain is the hostname with its first
        label removed. Wildcards and any hostname without a subdomain label
        are ignored rather than guessed at.
        """
        candidates: set[str] = set()
        for hostname in self.gateway_route_hostnames():
            if hostname.startswith("*"):
                continue
            labels = hostname.split(".")
            if len(labels) < 3:
                # Not `<service>.<domain>`; nothing to strip with confidence.
                continue
            candidates.add(".".join(labels[1:]))

        if len(candidates) == 1:
            return candidates.pop()
        if len(candidates) > 1:
            raise ValueError(
                "HTTPRoutes attached to Gateway "
                f"{constants.GATEWAY_NAMESPACE}/{constants.GATEWAY_NAME} name more "
                f"than one platform domain ({sorted(candidates)}); cannot determine "
                "which one this cluster is"
            )

        return self._domain_from_certificate()

    def _domain_from_certificate(self) -> str:
        """Fallback for a cluster whose HTTPRoutes are not readable."""
        try:
            cert = self.custom.get_namespaced_custom_object(
                group=CERTMANAGER_GROUP,
                version=CERTMANAGER_VERSION,
                namespace=constants.GATEWAY_NAMESPACE,
                plural=CERTIFICATE_PLURAL,
                name=constants.GATEWAY_CERTIFICATE_NAME,
            )
        except ApiException as error:
            if error.status != NOT_FOUND_STATUS:
                raise
            raise ValueError(
                "could not determine the platform domain: no HTTPRoute attached to "
                f"Gateway {constants.GATEWAY_NAMESPACE}/{constants.GATEWAY_NAME} "
                "carries a <service>.<domain> hostname, and Certificate "
                f"{constants.GATEWAY_NAMESPACE}/"
                f"{constants.GATEWAY_CERTIFICATE_NAME} does not exist "
                "(expected on a cluster deployed with certificate.type: existing). "
                "Is this a Nebari cluster, and does the kubeconfig have read access "
                "to HTTPRoutes?"
            ) from error

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

    def storage_class_names(self) -> list[str]:
        return [sc.metadata.name for sc in self.storage.list_storage_class().items]

    def has_longhorn(self) -> bool:
        """Whether Longhorn is installed on this cluster.

        Keyed off the `longhorn` StorageClass rather than an ArgoCD
        Application, because Longhorn core is NOT one: there is no
        apps/longhorn.yaml in pkg/argocd/templates/apps, only
        apps/longhorn-backup.yaml, so require_app() cannot answer this
        question. The StorageClass is preferred over the longhorn-system
        namespace because the namespace can outlive an uninstall (a
        terminating or leftover-but-empty namespace would report Longhorn
        present), and because the StorageClass is the thing the storage
        journeys actually consume.
        """
        return LONGHORN_STORAGE_CLASS in self.storage_class_names()

    def require_longhorn(self) -> None:
        """Skip the calling test when Longhorn is not installed.

        Longhorn is optional: a Nebari cluster on EKS or GKE may run entirely
        on the cloud provider's storage, with no Longhorn StorageClass, no
        Longhorn UI, no `longhorn` OIDC client and no `longhorn-admins` group.
        None of that is a failure.
        """
        if not self.has_longhorn():
            pytest.skip(
                f"Longhorn is not installed on this cluster: no {LONGHORN_STORAGE_CLASS!r} "
                "StorageClass exists"
            )

    def default_storage_class(self) -> str:
        """The class marked default, else Longhorn's."""
        for sc in self.storage.list_storage_class().items:
            annotations = sc.metadata.annotations or {}
            if annotations.get(STORAGE_DEFAULT_ANNOTATION) == "true":
                return sc.metadata.name
        return FALLBACK_STORAGE_CLASS

    def longhorn_volume(self, pv_name: str) -> dict:
        """The Longhorn Volume backing a PersistentVolume."""
        return self.custom.get_namespaced_custom_object(
            group=LONGHORN_GROUP,
            version=LONGHORN_VERSION,
            namespace=constants.LONGHORN_NAMESPACE,
            plural=VOLUME_PLURAL,
            name=pv_name,
        )
