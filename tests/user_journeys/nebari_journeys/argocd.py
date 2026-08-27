"""ArgoCD application actions."""

from dataclasses import dataclass

# The app-of-apps manages the others; asserting on it duplicates its children
# and reports OutOfSync for reasons that are not a foundational-software fault.
ROOT_APP_NAME = "nebari-root"

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


def foundational_applications(cluster) -> list[Application]:
    """Every foundational application except the root app-of-apps."""
    return [
        Application.from_object(obj)
        for obj in cluster.applications()
        if obj["metadata"]["name"] != ROOT_APP_NAME
    ]
