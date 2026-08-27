"""Smoke journey.

Runs first. If this fails alongside a journey, the cluster is broken;
if only a journey fails, that feature is broken.
"""

from nebari_journeys.argocd import foundational_applications


def test_operator_sees_every_foundational_app_synced_and_healthy(cluster):
    """Only Applications labeled app.kubernetes.io/part-of=nebari-foundational
    count. An operator's own Application in the argocd namespace is not
    Nebari's foundational software and must not fail this journey."""
    apps = foundational_applications(cluster)
    assert apps, (
        "no ArgoCD application in the argocd namespace carries "
        "app.kubernetes.io/part-of=nebari-foundational; either ArgoCD has not "
        "bootstrapped or this is not a Nebari cluster"
    )

    unhealthy = [
        f"{a.name}: sync={a.sync_status} health={a.health_status}"
        for a in apps
        if not (a.is_synced() and a.is_healthy())
    ]
    assert not unhealthy, "foundational applications not ready:\n" + "\n".join(
        unhealthy
    )
