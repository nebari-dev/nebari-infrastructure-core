"""Namespaced Kubernetes actions.

Everything a journey creates is namespaced and carries the journey
label, so the blast radius is bounded and leftovers are identifiable.
No action here asserts; assertions belong in the test files.
"""

import uuid
from dataclasses import dataclass

from kubernetes.client.rest import ApiException
from kubernetes.stream import stream

from nebari_journeys import constants
from nebari_journeys.waits import wait_for_condition

JOURNEY_LABELS = {constants.JOURNEY_LABEL_KEY: constants.JOURNEY_LABEL_VALUE}

# A small image that is already present on Nebari nodes and stays running.
UTILITY_IMAGE = "busybox:1.36"
MOUNT_PATH = "/data"

# Second, independent guard for the sweep: a labeled namespace is only
# deleted if its name also matches this prefix. One label is too thin a
# basis for an irreversible, unattended delete against a cluster that may
# be production; the generator and the guard both build from this constant
# so they can never disagree.
SCRATCH_NAMESPACE_PREFIX = "nebari-journey-"


def scratch_namespace_name() -> str:
    return f"{SCRATCH_NAMESPACE_PREFIX}{uuid.uuid4().hex[:8]}"


@dataclass
class SweepResult:
    """What the sweep did. No logging in library code; the caller reports."""

    deleted: list[str]
    skipped: list[str]


def sweep_stale_namespaces(cluster) -> SweepResult:
    """Delete journey namespaces left behind by crashed runs.

    Only namespaces that carry the journey label AND whose name starts
    with SCRATCH_NAMESPACE_PREFIX are deleted. A labeled namespace with a
    foreign name is an anomaly: it is skipped, not deleted and not
    silently dropped.
    """
    selector = f"{constants.JOURNEY_LABEL_KEY}={constants.JOURNEY_LABEL_VALUE}"
    stale = cluster.core.list_namespace(label_selector=selector)
    names = [ns.metadata.name for ns in stale.items]

    deleted = []
    skipped = []
    for name in names:
        if name.startswith(SCRATCH_NAMESPACE_PREFIX):
            cluster.core.delete_namespace(name=name)
            deleted.append(name)
        else:
            skipped.append(name)
    return SweepResult(deleted=deleted, skipped=skipped)


@dataclass
class ScratchNamespace:
    """A disposable namespace and the actions available inside it."""

    cluster: object
    name: str

    def create(self) -> None:
        self.cluster.core.create_namespace(
            body={"metadata": {"name": self.name, "labels": dict(JOURNEY_LABELS)}}
        )

    def delete(self) -> None:
        try:
            self.cluster.core.delete_namespace(name=self.name)
        except ApiException as exc:
            if exc.status != 404:
                raise

    def request_volume(self, name: str, size: str, storage_class: str) -> str:
        self.cluster.core.create_namespaced_persistent_volume_claim(
            namespace=self.name,
            body={
                "metadata": {"name": name, "labels": dict(JOURNEY_LABELS)},
                "spec": {
                    "accessModes": ["ReadWriteOnce"],
                    "storageClassName": storage_class,
                    "resources": {"requests": {"storage": size}},
                },
            },
        )
        return name

    def wait_volume_bound(self, name: str, timeout: float = 180) -> None:
        def bound() -> bool:
            pvc = self.cluster.core.read_namespaced_persistent_volume_claim(
                name=name, namespace=self.name
            )
            return pvc.status.phase == "Bound"

        wait_for_condition(
            bound, timeout=timeout, description=f"PVC {self.name}/{name} to bind"
        )

    def run_pod(
        self,
        name: str,
        pvc_name: str | None = None,
        command: list[str] | None = None,
    ) -> str:
        container = {
            "name": "main",
            "image": UTILITY_IMAGE,
            "command": command or ["sh", "-c", "sleep 3600"],
            "volumeMounts": [],
        }
        volumes: list[dict] = []
        if pvc_name is not None:
            volumes.append(
                {"name": "data", "persistentVolumeClaim": {"claimName": pvc_name}}
            )
            container["volumeMounts"].append(
                {"name": "data", "mountPath": MOUNT_PATH}
            )

        self.cluster.core.create_namespaced_pod(
            namespace=self.name,
            body={
                "metadata": {"name": name, "labels": dict(JOURNEY_LABELS)},
                "spec": {
                    "restartPolicy": "Never",
                    "containers": [container],
                    "volumes": volumes,
                },
            },
        )
        return name

    def wait_pod_ready(self, name: str, timeout: float = 180) -> None:
        def ready() -> bool:
            pod = self.cluster.core.read_namespaced_pod(name=name, namespace=self.name)
            if pod.status.phase != "Running":
                return False
            conditions = pod.status.conditions or []
            return any(c.type == "Ready" and c.status == "True" for c in conditions)

        wait_for_condition(
            ready, timeout=timeout, description=f"pod {self.name}/{name} to be ready"
        )

    def delete_pod(self, name: str, timeout: float = 180) -> None:
        self.cluster.core.delete_namespaced_pod(name=name, namespace=self.name)

        def gone() -> bool:
            try:
                self.cluster.core.read_namespaced_pod(name=name, namespace=self.name)
            except ApiException as exc:
                return exc.status == 404
            return False

        wait_for_condition(
            gone, timeout=timeout, description=f"pod {self.name}/{name} to be deleted"
        )

    def exec(self, pod: str, command: list[str]) -> str:
        return stream(
            self.cluster.core.connect_get_namespaced_pod_exec,
            pod,
            self.name,
            command=command,
            stderr=True,
            stdin=False,
            stdout=True,
            tty=False,
        )
