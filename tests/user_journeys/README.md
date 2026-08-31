# User Journey Tests

End-to-end journeys verifying that Nebari's foundational software works, not
merely that it deployed. Design and rationale: [ADR-0017](../../docs/adr/0017-user-journey-tests-for-foundational-software.md).

## Layout

```
tests/user_journeys/
  pixi.toml, pyproject.toml, pytest.ini, pixi.lock   # environment and test config
  conftest.py            # only pytest_addoption for --keep-namespace
  nebari_journeys/       # the action library: constants, waits, sweep, cluster, trust, k8s, argocd, keycloak, ui
  tests_lib/              # unit tests OF the library, no cluster needed
  journeys/               # the journeys and their own conftest.py: test_smoke.py, test_identity.py, test_storage.py, test_tls.py
```

`journeys/` and `tests_lib/` each carry an `__init__.py`. Those are load
bearing: both directories hold a `test_storage.py`, and under pytest's default
`prepend` import mode a test module is named after its basename alone, so
without them a bare `pytest` from this directory dies at collection with
"import file mismatch". With them the modules are `journeys.test_storage` and
`tests_lib.test_storage`.
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
pytest                          # everything, including the library's tests
pixi run lint                   # ruff check .
pixi run fmt                    # ruff format .
```

## What each journey proves

| File | Journey |
|---|---|
| `test_smoke.py` | Every foundational ArgoCD app is Synced and Healthy. Runs first, so other failures are interpretable. |
| `test_identity.py` | The nebari realm is configured as promised: a new user can sign in to ArgoCD through Keycloak; Longhorn UI access follows `longhorn-admins` membership (5 realm-level checks plus 3 browser-driven checks). The ArgoCD sign-in journey is marked `@pytest.mark.requires_trusted_ca` and **known broken** on a self-signed cluster: ArgoCD SSO is UNVERIFIED there, since ArgoCD's server cannot trust the gateway certificate for server-side OIDC discovery (issue #490, root cause #447; issue #607 separately blocks in-cluster resolution of the issuer URL on the same shape). It skips there rather than failing, and runs (and can fail) normally on a cluster with a real issuing CA. |
| `test_storage.py` | Data survives pod replacement; the volume is genuinely replicated; backups are configured and functional (3 checks). All three need Longhorn and skip without it. |
| `test_tls.py` | The gateway's certificate validates against the plain system trust store, with no cluster-derived anchor. Marked `@pytest.mark.tls`; skips (does not fail) on a self-signed cluster, since a `CA:FALSE` leaf can never pass this by design (issue #447). |

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
   `ignore_https_errors=True`. Use the `trust_anchor` fixture; browser
   journeys additionally get an SPKI pin automatically (see "Chromium and
   self-signed clusters" below) -- do not add your own certificate bypass.
6. **Mark browser journeys `@pytest.mark.ui`**, journeys over 60 seconds
   `@pytest.mark.slow`, a journey whose SUBJECT is certificate validity
   or chain trust itself `@pytest.mark.tls` (see "Chromium and self-signed
   clusters" below) -- not a journey that merely happens to travel over TLS
   -- and a journey that depends on a THIRD PARTY, not the test runner,
   trusting the gateway certificate `@pytest.mark.requires_trusted_ca` (see
   "ArgoCD SSO on self-signed clusters" below).
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

## Cleanup of leftovers

Two sweeps, both reporting a `SweepResult(deleted, skipped, failed)`:
`deleted` is what was removed, `skipped` is what carried the journey marker
but failed the second guard and was deliberately left alone, and `failed` is
what the sweep tried and could not remove. A sweep never aborts on one bad
item, and everything but `deleted` is printed as an `!!! ANOMALY` line.

**Namespaces.** A session-scoped, autouse fixture (`sweep_leftovers` in
`journeys/conftest.py`) calls `sweep_stale_namespaces` at the start of every
run. A namespace is deleted only if it carries the journey label *and* its
name starts with `nebari-journey-`.

**Scratch realm users.** The `keycloak` fixture calls `sweep_scratch_users`
when it is first built. A run killed between `create_user` and its teardown
would otherwise leave an *enabled* user in a live realm with a password only
the dead process knew - and since
`test_longhorn_ui_admits_a_user_in_the_admins_group` adds its user to
`longhorn-admins` before logging in, that leftover can be a privileged one.
As with namespaces there are two guards: Keycloak's username search is an
*infix* match, so `journey-` also matches `prod-journey-admin`, and the
prefix is re-checked client side before anything is deleted.

The user sweep lives in the `keycloak` fixture rather than an autouse one on
purpose. It needs Keycloak, and Keycloak needs a reachable gateway; as an
autouse fixture it would force gateway reachability onto `test_smoke.py` and
the storage journeys, which speak only to the Kubernetes API and must keep
running when the gateway is unroutable.

## Keycloak admin tokens

Keycloak bootstraps the **master** realm with `accessTokenLifespan = 60`
(hardcoded in Keycloak's `ApplianceBootstrap.java`, not the 300 seconds a new
realm gets), and this suite authenticates against `/realms/master`. An admin
token is therefore valid for one minute, which a run with three browser
logins in it routinely outlives - and the very last admin call of a run is a
`delete_user` in a fixture teardown.

`Keycloak.token()` tracks expiry from the token response's `expires_in`,
refetches with a safety margin, and every admin call goes through
`Keycloak._send`, which refreshes once and retries on a `401`. Do not
reintroduce a token cached for the session: the resulting 401s read like a
platform authentication regression rather than a harness bug.

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

### Chromium and self-signed clusters: SPKI pinning, honestly

cert-manager's default `selfsigned-issuer` issues a self-signed **leaf**
(`CA:FALSE`), not a proper root CA (issue #447, #490). `requests`/OpenSSL
accepts a self-signed end-entity certificate as a trust anchor via `verify=`,
which is why the `requests`-based journeys work unmodified. Chromium/NSS does
not: a `CA:FALSE` certificate cannot be installed as a trust anchor at all, no
matter which NSS trust flag is used, so Chromium rejects it with
`ERR_CERT_AUTHORITY_INVALID` regardless of any OS/NSS trust-store change.

For the browser journeys, `nebari_journeys.trust.chromium_args` launches
Chromium with `--ignore-certificate-errors-spki-list=<hash>`, where `<hash>`
is `spki_sha256_b64()` of the same anchor PEM the `requests` journeys already
use. Read plainly, this pins Chromium's trust to the exact public key
(SubjectPublicKeyInfo) read from the cluster's own gateway secret over the
kubeconfig:

- A certificate presenting any other key -- a MITM, or the gateway rotating
  to a new key without updating the pin -- is still rejected outright. In
  that respect the anchor is not weakened.
- But for a connection that DOES present the pinned key, Chromium suppresses
  **every** certificate error on that connection, including ones full chain
  validation would still catch, such as a hostname mismatch. The pin proves
  "this is the key I read from the cluster", not "this certificate is
  actually valid for this hostname".

**This is weaker than full chain validation and is not sold as equivalent to
it.** It is applied only when a trust anchor was actually derived from the
cluster (a self-signed cluster); an ACME cluster gets no extra Chromium flags
at all, so it is driven exactly as a real user's browser would be. The real
fix is issue #447: once cert-manager issues a proper CA for the gateway,
Chromium validates the chain like any other browser, and the SPKI pin
(`spki_sha256_b64`, the flag it feeds, and this whole section) should be
removed.

Because the SPKI pin is table stakes for the browser journeys to run at all
on a self-signed cluster, but it explicitly does not restore hostname or
chain checking, a small number of journeys whose entire SUBJECT is
certificate validity or chain trust itself are marked `@pytest.mark.tls` (see
`pytest.ini`) and are **skipped**, not passed under a weaker guarantee, when
the derived anchor is a self-signed leaf
(`nebari_journeys.trust.is_self_signed_leaf`). Every other journey, including
every browser journey that merely happens to travel over TLS to get
somewhere, keeps running normally, pinned by the SPKI flag. The skip is
announced once per session as a `UserWarning` reading `self signed cert
detected, skipping tls tests`, whether or not any `tls`-marked test currently
exists, so an operator running locally can see that certificate validation
has been narrowed for the browser rather than discovering it only when a
`tls` journey silently skips later.

CI does not need to do anything special for Chromium: the SPKI flag comes
from the same kubeconfig-derived anchor the `Install cluster trust anchor for
the API journeys` step in `.github/workflows/ci.yml` reads for the
`requests`-based journeys, and is wired up entirely inside the test process
by `journeys/conftest.py`'s browser fixtures. Running the suite locally
against a self-signed cluster needs no manual trust-store setup either: point
`KUBECONFIG` at the cluster and run the journeys.

### ArgoCD SSO on self-signed clusters: known broken, not merely untested

The SPKI pin above fixes trust for the TEST RUNNER's own browser. It does
nothing for a THIRD PARTY that also has to trust the gateway certificate,
which is exactly the situation `test_a_new_user_can_sign_in_to_argocd_through_keycloak`
is in: ArgoCD's server performs OIDC discovery against the external Keycloak
URL (`https://keycloak.<domain>/realms/nebari`), as a separate server-side
HTTP call ArgoCD makes itself, not something Chromium's launch flags touch.
On a self-signed cluster ArgoCD does not trust that certificate and discovery
fails with:

```
failed to query provider "https://keycloak.<domain>/realms/nebari":
tls: failed to verify certificate: x509: certificate signed by unknown authority
```

This is issue [#490](https://github.com/nebari-dev/nebari-infrastructure-core/issues/490),
whose root cause is [#447](https://github.com/nebari-dev/nebari-infrastructure-core/issues/447):
cert-manager's default `selfsigned-issuer` issues a self-signed LEAF
(`CA:FALSE`), so there is no CA for ArgoCD to trust in the first place. A
second, separate blocker exists on the same cluster shape, issue
[#607](https://github.com/nebari-dev/nebari-infrastructure-core/issues/607):
the external hostname does not even resolve from inside the cluster, so
discovery can fail earlier still, with SERVFAIL. **ArgoCD SSO is UNVERIFIED
on a self-signed cluster, and is known broken, not merely untested.**

The journey is marked `@pytest.mark.requires_trusted_ca` and is **skipped**,
not failed, when the derived gateway anchor is a self-signed leaf (the
`skip_trusted_ca_marked_tests_on_self_signed` fixture in
`journeys/conftest.py`, keyed off the same
`nebari_journeys.trust.is_self_signed_leaf` check the `tls` marker uses).
This is a different shape from the `tls` skip: `tls`-marked journeys are
skipped because the TEST RUNNER cannot validate the chain; this journey is
skipped because ArgoCD's server cannot, which no SPKI pin in this process
can fix. The other identity journeys (realm configuration, groups scope,
OIDC clients, redirect URIs) talk to Keycloak directly from the test runner
over the trust anchor the suite derives, so they are unaffected and keep
running on a self-signed cluster. On a cluster with a real issuing CA, the
sign-in journey runs normally and still fails if SSO is actually broken
there -- the skip is scoped to a cluster shape that cannot physically work,
not to the symptom.

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
- **Chromium trust no longer goes through the OS/NSS store at all.** An
  earlier version of the CI trust step also imported the anchor into the
  per-user NSS DB via `certutil -A -t "C,,"` (trusted CA). That could never
  have worked: cert-manager's default `selfsigned-issuer` issues a leaf with
  `CA:FALSE`, and NSS correctly refuses to use a `CA:FALSE` certificate as a
  trust anchor under any trust flag, `-t "P,,"` (trusted peer) included,
  because a self-signed non-CA is not a chain Chromium can walk. The failure
  was masked because that half of the step was deliberately best-effort. The
  CI step now only installs the anchor into the runner's system trust store
  for the `requests`-based journeys; Chromium trust is handled entirely by
  the SPKI pin in `nebari_journeys.trust.chromium_args` (see "Chromium and
  self-signed clusters" above). If a browser journey fails with
  `ERR_CERT_AUTHORITY_INVALID`, the SPKI flag is not reaching Chromium's
  launch args -- check `browser_type_launch_args` in `journeys/conftest.py`.
  Do not reach for `ignore_https_errors`: it is forbidden here.
- **Platform domain is derived by longest common suffix, not by stripping a
  fixed number of labels.** An earlier version of `domain()` stripped exactly
  one DNS label from every HTTPRoute hostname and required the results to
  agree. That broke on every real cluster: the landing page route
  (`nebari-system/nebari-landing-route`) serves the bare apex domain with no
  service label at all, one fewer label than `argocd.<domain>`,
  `keycloak.<domain>`, and `longhorn.<domain>`, so stripping one label from
  the apex produced a different (and wrong) value and `domain()` raised on
  every session setup. `domain()` now computes the longest common suffix, by
  DNS label, across every hostname served by an HTTPRoute attached to the
  gateway, which handles the apex route and any subdomain route uniformly. A
  transient cert-manager ACME challenge route for the apex is just another
  apex hostname and is absorbed by the same rule, so it is not a special
  case.
- **The journeys have never been executed against a live cluster.** Every
  journey has been exercised in isolation with tests_lib, but the suite as a
  whole (`journeys/`) has not yet had a run against a real deployed Nebari
  cluster. Treat the first CI or local run as a validation pass, not a status
  check: failures may be journey bugs surfacing for the first time rather
  than regressions.
