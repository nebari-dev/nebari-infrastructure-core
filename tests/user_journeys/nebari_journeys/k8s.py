"""Namespaced Kubernetes actions.

Everything a journey creates is namespaced and carries the journey
label, so the blast radius is bounded and leftovers are identifiable.
No action here asserts; assertions belong in the test files.

`SweepResult` is imported from `nebari_journeys.sweep` rather than defined
here, because `keycloak` reports its own sweep of leftover scratch users
in the same shape and must not import the Kubernetes client to do it. It
stays importable from this module, which is where callers expect it.
"""

import uuid
from dataclasses import dataclass

from kubernetes.client.rest import ApiException
from kubernetes.stream import stream

from nebari_journeys import constants
from nebari_journeys.sweep import SweepResult
from nebari_journeys.waits import wait_for_condition

JOURNEY_LABELS = {constants.JOURNEY_LABEL_KEY: constants.JOURNEY_LABEL_VALUE}

# The utility image every journey pod runs. It is small and stays running, but
# it is NOT preloaded on Nebari nodes: every journey pod pulls it from Docker
# Hub on first use. That dependency is an accepted risk, recorded here so that
# whoever debugs an ImagePullBackOff (rate limiting, an air-gapped cluster, a
# blocked registry) knows the pull is real and expected rather than a symptom
# of a broken node. wait_pod_ready() fails fast on that state rather than
# burning the whole timeout.
UTILITY_IMAGE = "busybox:1.36"
MOUNT_PATH = "/data"

# Container waiting reasons that never resolve on their own. Waiting out the
# full timeout on one of these turns a five-second diagnosis (the image cannot
# be pulled) into a three-minute "pod to be ready" timeout that names nothing.
# ErrImagePull is deliberately NOT here: it is the transient state kubelet
# passes through before settling into ImagePullBackOff, and failing on it
# would make a single slow or retried pull look like an outage.
TERMINAL_WAITING_REASONS = frozenset(
    {
        "ImagePullBackOff",
        "InvalidImageName",
        "CreateContainerConfigError",
        "CreateContainerError",
        "CrashLoopBackOff",
    }
)

# Pod phases from which a pod can never become Ready.
TERMINAL_POD_PHASES = frozenset({"Failed", "Succeeded"})

# Second, independent guard for the sweep: a labeled namespace is only
# deleted if its name also matches this prefix. One label is too thin a
# basis for an irreversible, unattended delete against a cluster that may
# be production; the generator and the guard both build from this constant
# so they can never disagree.
SCRATCH_NAMESPACE_PREFIX = "nebari-journey-"


def terminal_waiting_reason(pod) -> str | None:
    """The container waiting reason a pod is permanently stuck in, if any."""
    for status in pod.status.container_statuses or []:
        waiting = getattr(status.state, "waiting", None) if status.state else None
        if waiting is not None and waiting.reason in TERMINAL_WAITING_REASONS:
            return waiting.reason
    return None


def pod_failure_detail(pod) -> str:
    """Whatever the pod says about why it is not running, for an error message."""
    parts = [f"phase={pod.status.phase!r}"]
    if pod.status.reason:
        parts.append(f"reason={pod.status.reason!r}")
    for status in pod.status.container_statuses or []:
        state = status.state
        waiting = getattr(state, "waiting", None) if state else None
        if waiting is not None and waiting.reason:
            parts.append(f"container {status.name} waiting: {waiting.reason}")
            if waiting.message:
                parts.append(str(waiting.message))
        terminated = getattr(state, "terminated", None) if state else None
        if terminated is not None and terminated.reason:
            parts.append(
                f"container {status.name} terminated: {terminated.reason} "
                f"(exit {terminated.exit_code})"
            )
    return "; ".join(parts)


def scratch_namespace_name() -> str:
    return f"{SCRATCH_NAMESPACE_PREFIX}{uuid.uuid4().hex[:8]}"


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

    result = SweepResult()
    for name in names:
        if not name.startswith(SCRATCH_NAMESPACE_PREFIX):
            result.skipped.append(name)
            continue
        try:
            cluster.core.delete_namespace(name=name)
        except ApiException as exc:
            # 404 means something else already removed it, which is the
            # outcome the sweep wanted; anything else left it in place and
            # the caller has to be told. Either way the sweep continues:
            # one undeletable namespace must not strand the rest.
            if exc.status == 404:
                result.deleted.append(name)
            else:
                result.failed.append(name)
        else:
            result.deleted.append(name)
    return result


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
            container["volumeMounts"].append({"name": "data", "mountPath": MOUNT_PATH})

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
        """Wait for a pod to be Ready, failing fast on states that never will.

        A pod that cannot pull its image, or that has already terminated, is
        never going to become Ready. Polling it until the timeout expires
        reports only "timed out waiting for pod X to be ready", which says
        nothing about why. Raising immediately with the phase and the
        container's waiting reason turns that into a one-line diagnosis in a
        CI log.

        A 404 here means the opposite of what it means in delete_pod: right
        after create_namespaced_pod returns, the API server can briefly not
        yet serve the object back to a subsequent read (observed on EKS).
        That is "not visible yet", not "gone", so it is treated as NOT
        READY and polling continues. Every other ApiException still
        propagates, and a terminal phase or waiting reason still raises
        immediately even if a 404 was seen on an earlier poll.
        """

        def ready() -> bool:
            try:
                pod = self.cluster.core.read_namespaced_pod(
                    name=name, namespace=self.name
                )
            except ApiException as exc:
                if exc.status == 404:
                    return False
                raise
            phase = pod.status.phase

            if phase in TERMINAL_POD_PHASES:
                raise RuntimeError(
                    f"pod {self.name}/{name} reached phase {phase!r} and will never "
                    f"become ready ({pod_failure_detail(pod)})"
                )

            reason = terminal_waiting_reason(pod)
            if reason is not None:
                raise RuntimeError(
                    f"pod {self.name}/{name} is stuck in {reason!r} and will never "
                    f"become ready ({pod_failure_detail(pod)})"
                )

            if phase != "Running":
                return False
            conditions = pod.status.conditions or []
            return any(c.type == "Ready" and c.status == "True" for c in conditions)

        wait_for_condition(
            ready, timeout=timeout, description=f"pod {self.name}/{name} to be ready"
        )

    def delete_pod(self, name: str, timeout: float = 180) -> None:
        self.cluster.core.delete_namespaced_pod(name=name, namespace=self.name)

        def gone() -> bool:
            # Here a 404 means the opposite of what it means in
            # wait_pod_ready: this poll starts only after a successful
            # delete, so a 404 means the object is actually gone, not that
            # it is not visible yet. Both interpretations are correct for
            # their own call site; do not "harmonise" them.
            try:
                self.cluster.core.read_namespaced_pod(name=name, namespace=self.name)
            except ApiException as exc:
                return exc.status == 404
            return False

        wait_for_condition(
            gone, timeout=timeout, description=f"pod {self.name}/{name} to be deleted"
        )

    def exec(self, pod: str, command: list[str]) -> str:
        """Run a command in a pod and return its combined output.

        HAZARD, read before adding a caller: the exit code is NOT checked and
        stderr is merged into the returned string. A command that fails
        returns its error text as if it were content, so
        `if ns.exec(...) != ""` is a false pass, and `assert expected in
        ns.exec(...)` is the only safe shape.

        Both current callers use that safe shape: they assert on a specific
        canary string, which a shell error message cannot satisfy. A caller
        that needs to know whether the command SUCCEEDED must not use this
        method as-is; give it a checked variant built on
        `stream(..., _preload_content=False)`, which exposes `returncode` and
        separates the channels.
        """
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
