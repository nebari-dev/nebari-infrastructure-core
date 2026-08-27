# User Journey Tests

End-to-end journeys verifying that Nebari's foundational software works, not
merely that it deployed. Design and rationale: [ADR-0017](../../docs/adr/0017-user-journey-tests-for-foundational-software.md).

## Layout

```
tests/user_journeys/
  pixi.toml, pyproject.toml, pytest.ini, pixi.lock   # environment and test config
  conftest.py            # only pytest_addoption for --keep-namespace
  nebari_journeys/       # the action library: constants, waits, cluster, trust, k8s, argocd, keycloak, ui
  tests_lib/              # unit tests OF the library, 140 tests, no cluster needed
  journeys/               # the journeys and their own conftest.py: test_smoke.py, test_identity.py, test_storage.py
```

## Running

A kubeconfig pointing at a deployed Nebari cluster is the only required input.

```bash
export KUBECONFIG=/path/to/kubeconfig
make test-journeys              # everything, browsers included
make test-journeys-lib          # tests of the library itself, no cluster needed
```

Finer control, from `tests/user_journeys/`:

```bash
pixi run test-api               # skip browser journeys, no Chromium download
pixi run test -k storage        # one slice
pixi run test --keep-namespace  # leave the scratch namespace for debugging
pixi run install-browsers       # install Chromium for Playwright
pixi run lint                   # ruff check .
pixi run fmt                    # ruff format .
```

## What each journey proves

| File | Journey |
|---|---|
| `test_smoke.py` | Every foundational ArgoCD app is Synced and Healthy. Runs first, so other failures are interpretable. |
| `test_identity.py` | The nebari realm is configured as promised: a new user can sign in to ArgoCD through Keycloak; Longhorn UI access follows `longhorn-admins` membership (5 realm-level checks plus 3 browser-driven checks). |
| `test_storage.py` | Data survives pod replacement; the volume is genuinely replicated; backups are configured and functional (3 checks). All three need Longhorn and skip without it. |

## Adding a journey

1. **Put the verb in the library, the assertion in the test.** An `assert` inside
   `nebari_journeys/` is a design error. Tests read as stories; the library
   supplies the vocabulary.
2. **Name the test after the user's goal**, not the mechanism:
   `test_a_users_data_survives_their_pod`, not `test_pvc_rebind`.
3. **Skip, do not fail, when an optional component is absent.** Call
   `cluster.require_app("<argocd-app-name>")` first. Longhorn is the
   exception: Longhorn core is not an ArgoCD Application (there is no
   `apps/longhorn.yaml`, only `apps/longhorn-backup.yaml`), so use
   `cluster.require_longhorn()`, which keys off the `longhorn` StorageClass.
   Where only part of a journey is Longhorn-specific, gate that part on
   `cluster.has_longhorn()` so the rest still runs.
4. **Work inside `scratch_namespace`.** Never create cluster-scoped resources
   and never write outside the namespace.
5. **Never disable TLS verification.** No `verify=False`, no
   `ignore_https_errors=True`. Use the `trust_anchor` fixture.
6. **Mark browser journeys `@pytest.mark.ui`** and journeys over 60 seconds
   `@pytest.mark.slow`.
7. **Prefer `scratch_user` over admin credentials**, so failure artifacts carry
   as little as possible.
8. **Write the assertion message for someone reading a CI log** with no access
   to the cluster. State what was expected and what was found.

## Constants

`nebari_journeys/constants.py` mirrors Go constants and is pinned by
`pkg/argocd/python_constants_test.go`. Changing a value on one side without the
other fails the Go build. Add new mirrored constants to both the Python module
and that test's table.

A name that mirrors Go or a manifest template belongs in `constants.py` and
nowhere else, so the contract test can see it. Three pinning patterns exist:

| Source | Test |
|---|---|
| A Go constant | `TestPythonConstantsMatchGo` |
| `metadata.name` in a manifest template | `TestPythonResourceNamesMatchTemplates` |
| A shell literal in `realm-setup-job.yaml` | `TestPythonRealmLiteralsMatchTemplate` |

`TestPythonConstantsEnrollment` fails when a constant is added without landing
in one of them or being listed as suite-owned.

## Namespace cleanup

A session-scoped, autouse fixture (`sweep_leftovers` in `journeys/conftest.py`)
calls `sweep_stale_namespaces` at the start of every run to remove namespaces
left behind by earlier crashed runs. It returns a `SweepResult(deleted,
skipped)`: `deleted` lists what it actually removed, `skipped` lists any
namespace that carries the journey label but does not match the
`nebari-journey-` prefix, which it deliberately refuses to delete and reports
as an anomaly for a human to investigate.

## Optional components

Longhorn is optional. A Nebari cluster on EKS or GKE can run entirely on the
cloud provider's storage, in which case there is no `longhorn` StorageClass,
no Longhorn UI, no `longhorn` OIDC client and no `longhorn-admins` group.
None of that is a failure, so:

- the three storage journeys call `cluster.require_longhorn()` and skip
- the two Longhorn browser journeys skip
- the realm group and OIDC client journeys assert the ArgoCD half
  unconditionally and skip only the Longhorn-specific assertion

The storage journeys request the `longhorn` StorageClass by name rather than
`cluster.default_storage_class()`. On EKS the default is `gp2`/`gp3` and on
GKE `standard`, so using the default would silently test the cloud provider's
provisioner while claiming to test Longhorn's promise.

## Domain and trust anchor discovery

Both the platform domain and the gateway's TLS secret are discovered from the
cluster, not assumed:

- **Domain** comes from the hostnames of the HTTPRoutes attached to the
  `nebari-gateway` Gateway (`argocd.<domain>`, `keycloak.<domain>`, ...),
  falling back to the gateway Certificate's `commonName`. The Certificate is
  NOT the primary source because it does not exist on every supported shape:
  with `certificate.type: existing`, NIC never renders
  `gateway-certificate.yaml`, and reading it first made every journey error
  at session setup on that shape, the smoke journey included.
- **Trust anchor** comes from the secret the Gateway's own
  `listeners[].tls.certificateRefs` names, not from a hardcoded
  `nebari-gateway-tls` in `envoy-gateway-system`. Both the name and the
  namespace are operator-configurable (`certificate.secret_name`,
  `certificate.existing_secret`), and ADR-0017 requires the anchor to follow
  the operator's secret.

The suite never sets `ignore_https_errors` or `verify=False`. The anchor
prefers `ca.crt` and falls back to `tls.crt` for a self-signed leaf (NOT
`nebari-trust-bundle`). A missing secret is a legitimate cluster shape (a
publicly trusted ACME certificate) and simply falls back to the system trust
store.

Chromium does not accept a custom CA through any Playwright context option; it
reads the OS/NSS trust store instead. CI installs the anchor into the runner's
trust store before running the journeys (see the "Install cluster trust
anchor for Chromium" step in `.github/workflows/ci.yml`), reusing
`trust.trust_anchor_pem` so the CI path and the `trust_anchor` fixture agree.
Running the suite locally against a self-signed cluster requires the same
step: install the gateway's CA into your own OS trust store before running
Chromium journeys, rather than reaching for `ignore_https_errors`.

## Security posture

The suite needs a near-cluster-admin kubeconfig: it reads platform admin
credentials and creates namespaces. Treat its kubeconfig as a cluster-admin
credential.

The `test` pixi task (the only one that launches a browser) runs with
`--tracing=retain-on-failure --screenshot=only-on-failure
--video=retain-on-failure`, so a trace, screenshot and video are captured
only when a browser journey fails, landing in `test-results/`. In CI these
are uploaded only when the job fails and are retained for three days, then
deleted. `test-api` and `test-lib` never launch a browser and capture
nothing.

That short retention window is deliberate, not a default left unconsidered: a
Playwright trace or video from a failed login journey can contain the
throwaway user's password, and Playwright has no trace redaction API. Short
retention plus throwaway credentials (`scratch_user`, not admin credentials,
wherever a journey can use one) are the actual control. Do not raise
`retention-days` without first solving redaction.

## Dependencies

The conda-forge dependency is `python-kubernetes`, not `kubernetes`. The
`kubernetes` conda-forge package ships CLI binaries (`kubectl` and friends),
not the Python client library; the import inside the library is still
`import kubernetes`, since that is the name the `python-kubernetes` package
installs under.

## Known caveats

- **The Longhorn UI marker is an unverified guess.** `LONGHORN_UI_MARKER =
  "Recurring Job"` in `nebari_journeys/ui.py` is a guess at a string that
  appears in Longhorn's sidebar. The detection *logic* in the Longhorn
  authorization journeys is sound (it distinguishes access from denial by
  whether this marker is visible), but the marker *string* has not been
  confirmed against a real Longhorn UI. Confirm it on the first run against a
  live cluster and correct it if the sidebar copy differs.
- **The ArgoCD OIDC login path is an unverified guess.**
  `ARGOCD_OIDC_LOGIN_PATH = "/auth/login"` in `nebari_journeys/ui.py` is the
  path the ArgoCD sign-in journey navigates to in order to force the OIDC
  flow. Navigating to the bare `argocd.<domain>` host is definitely wrong:
  ArgoCD's own `/login` page renders ArgoCD's LOCAL username/password form
  plus a separate "LOG IN VIA <provider>" button and does not auto-redirect,
  so the Keycloak selectors either time out or, worse, submit a Keycloak user
  to ArgoCD's local login. `/auth/login` is argocd-server's OIDC login
  handler, but that has not been confirmed against a live cluster. The
  journey asserts it ended up back on the ArgoCD host and not on Keycloak, so
  a wrong path fails loudly rather than passing quietly.
- **The CI trust step's NSS half is unverified.** With the default
  `selfsigned-issuer`, cert-manager issues a leaf with `CA:FALSE`. OpenSSL
  generally accepts a self-signed end-entity certificate found in the trust
  store, so the `requests` path should be fine, but NSS is stricter and
  Chromium reads NSS. The CI step therefore installs the anchor into BOTH
  `/usr/local/share/ca-certificates` and the per-user NSS DB via `certutil`,
  and the NSS half is best-effort so it cannot turn a green run red. If a
  browser journey fails with `ERR_CERT_AUTHORITY_INVALID`, fix the trust
  store or give the issuer `isCA: true`. Do not reach for
  `ignore_https_errors`: it is forbidden here.
- **ACME challenges may cause domain derivation to fail.** Platform domain
  discovery strips one DNS label from HTTPRoute hostnames. During ACME
  issuance or renewal, cert-manager attaches temporary HTTP-01 challenge
  routes to the gateway. On a cluster with `nebari.example.com`, these strip
  to `example.com`, failing to match the other routes. `domain()` then raises
  "more than one platform domain" and every journey errors at session setup.
  The failure is loud, not silent, and transient (only during challenge
  windows), but a run overlapping a renewal will fail unrelated to cluster
  health. If this happens, modify domain discovery to ignore ACME challenge
  paths or prefer the majority domain.
- **The journeys have never been executed against a live cluster.** Every
  journey has been exercised in isolation with tests_lib, but the suite as a
  whole (`journeys/`) has not yet had a run against a real deployed Nebari
  cluster. Treat the first CI or local run as a validation pass, not a status
  check: failures may be journey bugs surfacing for the first time rather
  than regressions.
