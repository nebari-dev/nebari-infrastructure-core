"""ArgoCD application actions."""

from dataclasses import dataclass, field

from nebari_journeys import constants

SYNCED = "Synced"
HEALTHY = "Healthy"
UNKNOWN = "Unknown"

# status.operationState.phase values that mean the sync OPERATION itself
# failed to run, as opposed to merely leaving the app out of sync with git.
# These are ArgoCD's own Application CRD phases, not Nebari-authored
# strings, so they are not mirrored constants.
ERROR_OPERATION_PHASES = frozenset({"Error", "Failed"})

# status.conditions[].type values that mean GitOps itself is broken (ArgoCD
# could not even compare or sync), rather than an app having drifted.
SYNC_ERROR_CONDITION_TYPES = frozenset({"ComparisonError", "SyncError"})


@dataclass
class Application:
    name: str
    sync_status: str
    health_status: str
    operation_phase: str | None = None
    condition_types: tuple[str, ...] = field(default_factory=tuple)

    @classmethod
    def from_object(cls, obj: dict) -> "Application":
        status = obj.get("status") or {}
        operation_state = status.get("operationState") or {}
        conditions = status.get("conditions") or []
        return cls(
            name=obj["metadata"]["name"],
            sync_status=status.get("sync", {}).get("status", UNKNOWN),
            health_status=status.get("health", {}).get("status", UNKNOWN),
            operation_phase=operation_state.get("phase"),
            condition_types=tuple(
                c.get("type") for c in conditions if c.get("type")
            ),
        )

    def is_synced(self) -> bool:
        return self.sync_status == SYNCED

    def is_healthy(self) -> bool:
        return self.health_status == HEALTHY

    def has_sync_error(self) -> bool:
        """True when GitOps itself is broken, not merely OutOfSync.

        Plain drift (`sync_status == "OutOfSync"`) is expected and
        tolerated: it answers "does the cluster match git", not "is it
        running". A sync ERROR is different: the sync operation itself
        failed (`operationState.phase` of `Error` or `Failed`), or ArgoCD
        could not even compare or sync (`ComparisonError` / `SyncError`
        condition). Either means the GitOps pipeline is broken, which is a
        real platform failure regardless of health.
        """
        if self.operation_phase in ERROR_OPERATION_PHASES:
            return True
        return any(t in SYNC_ERROR_CONDITION_TYPES for t in self.condition_types)


def is_foundational(obj: dict) -> bool:
    """Whether an Application is one NIC deploys, not one an operator added.

    Two conditions, both required:

    - It carries `app.kubernetes.io/part-of: nebari-foundational`. Every
      template in pkg/argocd/templates/apps sets this label, and an
      operator's own Application in the argocd namespace does not. Without
      the label check, an unrelated OutOfSync Application would fail the
      smoke journey with a message claiming foundational software is broken.
    - It is not the root app-of-apps. The root Application carries the same
      label (see rootAppOfAppsTemplate in pkg/argocd/bootstrap.go), but it
      manages the others: asserting on it duplicates its children and
      reports OutOfSync for reasons that are not a foundational-software
      fault.
    """
    metadata = obj.get("metadata") or {}
    if metadata.get("name") == constants.ROOT_APP_NAME:
        return False
    labels = metadata.get("labels") or {}
    return labels.get(constants.PART_OF_LABEL) == constants.FOUNDATIONAL_PART_OF


def foundational_applications(cluster) -> list[Application]:
    """Every foundational application except the root app-of-apps."""
    return [
        Application.from_object(obj)
        for obj in cluster.applications()
        if is_foundational(obj)
    ]
