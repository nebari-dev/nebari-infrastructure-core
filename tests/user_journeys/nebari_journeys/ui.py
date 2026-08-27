"""Playwright page objects.

Only journeys that genuinely need a browser live here.

Credential-leak note (verified against the installed Playwright Python
API, see .pixi/envs/default/lib/python3.14/site-packages/playwright/
sync_api/_generated.py): Playwright has no mechanism that redacts a
value from a trace. `page.add_locator_handler()` exists to dismiss
unpredictable overlays and dialogs that block an action; it is not a
redaction feature, and registering one on the password field does
nothing to keep the typed value out of a trace snapshot. The `mask`
option lives on `page.screenshot()` / `expect(...).to_have_screenshot()`
only, is scoped to screenshots, and has no effect on the trace viewer's
DOM/network snapshots either. There is therefore no fake masking call
in this module.

The real controls, in order of how much they actually do:
- Journeys authenticate as `scratch_user`, a throwaway realm user with a
  `secrets`-generated password, not the platform admin credential. A
  trace that leaks this password only grants access to a user that gets
  deleted at the end of the run.
- CI keeps trace/artifact retention short, which bounds the exposure
  window if a trace is ever captured.

Residual risk, stated honestly: a Playwright trace captured from a
FAILED login journey (`test_a_new_user_can_sign_in_to_argocd_through_keycloak`
or either Longhorn journey) can contain the scratch user's password in
plaintext, because the trace records the value typed into the password
field. This is why these journeys must never be pointed at a real admin
credential, and why the password is generated fresh per run rather than
reused.
"""

KEYCLOAK_USERNAME_SELECTOR = "#username"
KEYCLOAK_PASSWORD_SELECTOR = "#password"
KEYCLOAK_SUBMIT_SELECTOR = "#kc-login"

# Positive marker of a successfully rendered Longhorn UI: a nav label from
# Longhorn's own dashboard sidebar (Dashboard / Volume / Node / Backup /
# Recurring Job / Setting), chosen because "Recurring Job" is an unusual
# two-word compound that a Keycloak login page, a gateway error page, or a
# generic "access denied" message is very unlikely to contain by accident.
#
# The STRING is provisional and MUST be confirmed against a live Longhorn
# UI on first real run: no cluster was available while writing this, so
# this is a reasoned guess about Longhorn's actual sidebar copy, not an
# observation. What is NOT provisional is the detection logic itself
# (assert the marker's presence for an admitted user, assert its absence
# for a denied one) -- that shape is sound regardless of which exact
# string ends up being right, and is deliberately keyed off a real,
# positive signal of Longhorn having rendered rather than off guessing the
# wording of a denial.
LONGHORN_UI_MARKER = "Recurring Job"

DENIED_STATUSES = frozenset({401, 403})


def is_access_denied(response_status: int) -> bool:
    return response_status in DENIED_STATUSES


def login_via_keycloak(page, url: str, username: str, password: str) -> None:
    """Drive the Keycloak login form and wait for the redirect to settle."""
    page.goto(url)
    page.fill(KEYCLOAK_USERNAME_SELECTOR, username)
    page.fill(KEYCLOAK_PASSWORD_SELECTOR, password)
    page.click(KEYCLOAK_SUBMIT_SELECTOR)
    page.wait_for_load_state("networkidle")
