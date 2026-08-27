"""Smoke journey.

Runs first. If this fails alongside a journey, the cluster is broken;
if only a journey fails, that feature is broken.
"""

from nebari_journeys.argocd import foundational_applications


def test_operator_sees_every_foundational_app_synced_and_healthy(cluster):
    apps = foundational_applications(cluster)
    assert apps, "no foundational ArgoCD applications found on this cluster"

    unhealthy = [
        f"{a.name}: sync={a.sync_status} health={a.health_status}"
        for a in apps
        if not (a.is_synced() and a.is_healthy())
    ]
    assert not unhealthy, "foundational applications not ready:\n" + "\n".join(unhealthy)
