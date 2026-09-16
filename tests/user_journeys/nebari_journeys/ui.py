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

from collections.abc import Callable
from urllib.parse import urlparse

from playwright.sync_api import TimeoutError as PlaywrightTimeoutError

KEYCLOAK_USERNAME_SELECTOR = "#username"
KEYCLOAK_PASSWORD_SELECTOR = "#password"
KEYCLOAK_SUBMIT_SELECTOR = "#kc-login"

# How long to wait for the Keycloak login form to appear after navigating.
# Distinct from the click timeout it replaces: the failure this bounds is
# "the OIDC redirect never happened", which deserves its own message.
FORM_TIMEOUT_MS = 30_000

# How long to wait, after submitting the form, for the OIDC round trip to
# land the browser back on the application's host.
REDIRECT_TIMEOUT_MS = 30_000

# Path that forces Argo CD to start an OIDC login instead of rendering its
# own local username/password form.
#
# This is PROVISIONAL in the same sense as LONGHORN_UI_MARKER below: no
# cluster was available to confirm it. The reasoning is that
# https://argocd.{domain} lands on Argo CD's own /login page, which shows the
# local login form plus a separate "LOG IN VIA <provider>" button and does
# NOT auto-redirect, so the Keycloak selectors below never resolve and the
# journey times out having tested nothing. argocd-server routes /auth/login
# to its OIDC login handler (the same endpoint the "LOG IN VIA" button links
# to, and the one `argocd login --sso` drives), so navigating straight there
# should produce the Keycloak form.
#
# What is NOT provisional is that navigating to the bare host is wrong for
# Argo CD: either it renders the local form (so the journey would submit a
# Keycloak user to Argo CD's LOCAL login, which is not what it claims to
# verify) or the click times out. Confirm the exact path on the first run
# against a live cluster; the journey asserts it ended up back on the Argo CD
# host and not on Keycloak, so a wrong path fails loudly rather than passing.
ARGOCD_OIDC_LOGIN_PATH = "/auth/login"

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

# How much of the page body to include in the diagnostic raised when the
# Keycloak form never appears. Enough to show a rendered error message
# (ArgoCD's OIDC-discovery failure body is a couple of lines) without
# risking a huge dump of an unrelated page landing in a CI log.
PAGE_BODY_DIAGNOSTIC_CHARS = 2000


def is_access_denied(response_status: int) -> bool:
    return response_status in DENIED_STATUSES


def page_host(url: str) -> str:
    """Lowercased hostname of a URL, or "" when it has none.

    Journeys assert on where the browser ENDED UP, not just on what is
    rendered there. Being back on the application host, and not on the
    identity provider's, is the only evidence available from the page that
    the OIDC round trip actually completed.
    """
    return (urlparse(url).hostname or "").lower()


def returned_to(host: str) -> "Callable[[str], bool]":
    """Predicate: has the browser landed back on `host`?

    The completion signal for an OIDC round trip. Extracted so it can be
    unit tested, and so both the wait and the journeys' assertions judge
    "did we get back" the same way.
    """

    def check(url: str) -> bool:
        return page_host(url) == host.lower()

    return check


def login_via_keycloak(page, url: str, username: str, password: str) -> None:
    """Drive the Keycloak login form and wait for the redirect to settle.

    Waits for the username field before filling it. Without that wait a URL
    that does not redirect to Keycloak (Argo CD's own /login page, a gateway
    error page) fails on the submit click after the full Playwright timeout,
    with a message about a missing selector rather than about the redirect
    that never happened.

    When the username field never appears, the raw Playwright
    TimeoutError says only "locator #username not visible", which reads
    like the browser itself is broken, or worse, like a TLS problem, when
    the actual cause is usually a page that DID load: Argo CD's own error
    body for a server-side OIDC discovery failure (argocd-server unable to
    reach Keycloak internally), or its unredirected local login form. The
    page's own URL and a slice of its rendered body are far more useful
    than the selector timeout, so this catches the timeout and re-raises
    with both attached, making clear the browser reached a host and
    rendered something, rather than that TLS or the harness broke.
    """
    page.goto(url)
    try:
        page.wait_for_selector(KEYCLOAK_USERNAME_SELECTOR, timeout=FORM_TIMEOUT_MS)
    except PlaywrightTimeoutError as error:
        body = page.content()[:PAGE_BODY_DIAGNOSTIC_CHARS]
        raise RuntimeError(
            f"the Keycloak login form never appeared after navigating to "
            f"{url!r}. The browser DID reach and render a page (this is not "
            f"a TLS or harness failure): it ended up at {page.url!r} with "
            f"this body:\n{body}"
        ) from error
    page.fill(KEYCLOAK_USERNAME_SELECTOR, username)
    page.fill(KEYCLOAK_PASSWORD_SELECTOR, password)
    page.click(KEYCLOAK_SUBMIT_SELECTOR)

    # Wait for the browser to arrive back on the application's own host,
    # NOT for the network to go quiet.
    #
    # `wait_for_load_state("networkidle")` was here first and is wrong for
    # this page. Playwright's own documentation marks it DISCOURAGED --
    # "Don't use this method for testing, rely on web assertions to assess
    # readiness instead" -- because it waits for 500ms with no network
    # connections at all, and Argo CD's application view holds a watch
    # stream open for as long as it is displayed. On that page the network
    # may never go idle, so the wait would burn its full timeout and fail a
    # successful login. It happens to have worked on the clusters this was
    # first run against, which is exactly the kind of luck not to build on.
    #
    # A timeout here is deliberately NOT raised. Every journey that calls
    # this asserts on where the browser ended up and says precisely what
    # each outcome means -- still on Keycloak, on some third host, on a
    # login page. Those messages are far more useful than a Playwright
    # timeout, so this hands control back and lets them speak.
    try:
        page.wait_for_url(returned_to(page_host(url)), timeout=REDIRECT_TIMEOUT_MS)
    except PlaywrightTimeoutError:
        return
