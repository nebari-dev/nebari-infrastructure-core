"""Smoke journey.

Runs first. If this fails alongside a journey, the cluster is broken;
if only a journey fails, that feature is broken.
"""

import warnings

from nebari_journeys.argocd import foundational_applications


def test_operator_sees_every_foundational_app_healthy(cluster):
    """Health answers "is it running"; sync answers "does the cluster match
    git". This journey triages the former.

    Only Applications labeled app.kubernetes.io/part-of=nebari-foundational
    count. An operator's own Application in the argocd namespace is not
    Nebari's foundational software and must not fail this journey.

    Plain `OutOfSync` does NOT fail this journey: ArgoCD reports it for
    trivial, insignificant drift while the app is genuinely Healthy and
    working (see ADR-0017). Drift is still made visible via a warning rather
    than asserted on, so it stays discoverable every run without training
    people to ignore a red suite. A GENUINE sync error -- the sync
    operation itself erroring or failing, or ArgoCD being unable to even
    compare or sync -- means GitOps itself is broken, which is a real
    platform failure and does fail this journey.
    """
    apps = foundational_applications(cluster)
    assert apps, (
        "no ArgoCD application in the argocd namespace carries "
        "app.kubernetes.io/part-of=nebari-foundational; either ArgoCD has not "
        "bootstrapped or this is not a Nebari cluster"
    )

    drifted = [a.name for a in apps if not a.is_synced()]
    if drifted:
        warnings.warn(
            "foundational applications are OutOfSync (not failing this journey; "
            "see ADR-0017): " + ", ".join(drifted),
            UserWarning,
            stacklevel=2,
        )

    unhealthy = [
        f"{a.name}: health={a.health_status}" for a in apps if not a.is_healthy()
    ]
    sync_errors = [
        f"{a.name}: operation phase={a.operation_phase!r}, "
        f"error conditions={[t for t in a.condition_types if t in ('ComparisonError', 'SyncError')]}"
        for a in apps
        if a.has_sync_error()
    ]

    failures = []
    if unhealthy:
        failures.append(
            "foundational applications are not Healthy:\n" + "\n".join(unhealthy)
        )
    if sync_errors:
        failures.append(
            "foundational applications have a GitOps sync error (not mere "
            "drift; GitOps itself is broken):\n" + "\n".join(sync_errors)
        )

    assert not failures, "\n\n".join(failures)
