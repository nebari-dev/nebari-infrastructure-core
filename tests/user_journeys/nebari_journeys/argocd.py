"""ArgoCD application actions."""

from dataclasses import dataclass

from nebari_journeys import constants

SYNCED = "Synced"
HEALTHY = "Healthy"
UNKNOWN = "Unknown"


@dataclass
class Application:
    name: str
    sync_status: str
    health_status: str

    @classmethod
    def from_object(cls, obj: dict) -> "Application":
        status = obj.get("status") or {}
        return cls(
            name=obj["metadata"]["name"],
            sync_status=status.get("sync", {}).get("status", UNKNOWN),
            health_status=status.get("health", {}).get("status", UNKNOWN),
        )

    def is_synced(self) -> bool:
        return self.sync_status == SYNCED

    def is_healthy(self) -> bool:
        return self.health_status == HEALTHY


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
