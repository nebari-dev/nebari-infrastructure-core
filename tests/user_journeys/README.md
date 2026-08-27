# User Journey Tests

End-to-end journeys verifying that Nebari's foundational software works, not
merely that it deployed. Design and rationale: [ADR-0017](../../docs/adr/0017-user-journey-tests-for-foundational-software.md).

## Layout

```
tests/user_journeys/
  pixi.toml, pyproject.toml, pytest.ini, pixi.lock   # environment and test config
  conftest.py            # only pytest_addoption for --keep-namespace
  nebari_journeys/       # the action library: constants, waits, cluster, trust, k8s, argocd, keycloak, ui
  tests_lib/              # unit tests OF the library, 95 tests, no cluster needed
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
| `test_storage.py` | Data survives pod replacement; the volume is genuinely replicated; backups are configured and functional (3 checks). |

## Adding a journey

1. **Put the verb in the library, the assertion in the test.** An `assert` inside
   `nebari_journeys/` is a design error. Tests read as stories; the library
   supplies the vocabulary.
2. **Name the test after the user's goal**, not the mechanism:
   `test_a_users_data_survives_their_pod`, not `test_pvc_rebind`.
3. **Skip, do not fail, when an optional component is absent.** Call
   `cluster.require_app("<argocd-app-name>")` first.
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

## Namespace cleanup

A session-scoped, autouse fixture (`sweep_leftovers` in `journeys/conftest.py`)
calls `sweep_stale_namespaces` at the start of every run to remove namespaces
left behind by earlier crashed runs. It returns a `SweepResult(deleted,
skipped)`: `deleted` lists what it actually removed, `skipped` lists any
namespace that carries the journey label but does not match the
`nebari-journey-` prefix, which it deliberately refuses to delete and reports
as an anomaly for a human to investigate.

## Trust anchor

The suite never sets `ignore_https_errors` or `verify=False`. Where a gateway
certificate is not publicly trusted, the trust anchor is read directly from
the gateway's own TLS secret, `nebari-gateway-tls` in `envoy-gateway-system`
(NOT `nebari-trust-bundle`), preferring `ca.crt` and falling back to `tls.crt`
for a self-signed leaf. A missing secret is a legitimate cluster shape (a
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
credential. Failure artifacts are retained for three days because browser
traces from a login journey are a credential-leak vector: Playwright has no
trace redaction API, so short retention plus throwaway credentials
(`scratch_user`, not admin credentials, wherever a journey can use one) are
the actual control.

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
- **The journeys have never been executed against a live cluster.** Every
  journey has been exercised in isolation with tests_lib, but the suite as a
  whole (`journeys/`) has not yet had a run against a real deployed Nebari
  cluster. Treat the first CI or local run as a validation pass, not a status
  check: failures may be journey bugs surfacing for the first time rather
  than regressions.
