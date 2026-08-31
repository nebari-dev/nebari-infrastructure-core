from unittest.mock import MagicMock

import pytest
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError

from nebari_journeys.ui import (
    ARGOCD_OIDC_LOGIN_PATH,
    KEYCLOAK_PASSWORD_SELECTOR,
    KEYCLOAK_SUBMIT_SELECTOR,
    KEYCLOAK_USERNAME_SELECTOR,
    is_access_denied,
    login_via_keycloak,
    page_host,
    returned_to,
)


def test_login_navigates_to_the_target_url():
    page = MagicMock()
    login_via_keycloak(page, "https://argocd.nebari.local", "u", "p")
    page.goto.assert_called_once_with("https://argocd.nebari.local")


def test_login_fills_username_and_password_then_submits():
    page = MagicMock()
    login_via_keycloak(page, "https://argocd.nebari.local", "alice", "pw")
    filled = [c.args for c in page.fill.call_args_list]
    assert (KEYCLOAK_USERNAME_SELECTOR, "alice") in filled
    assert (KEYCLOAK_PASSWORD_SELECTOR, "pw") in filled
    page.click.assert_called_once_with(KEYCLOAK_SUBMIT_SELECTOR)


def test_login_waits_for_the_browser_to_return_to_the_application_host():
    page = MagicMock()
    login_via_keycloak(page, "https://argocd.nebari.test/auth/login", "u", "p")

    page.wait_for_url.assert_called_once()
    predicate = page.wait_for_url.call_args.args[0]
    assert predicate("https://argocd.nebari.test/applications")
    assert not predicate("https://keycloak.nebari.test/realms/nebari")


def test_login_never_waits_on_networkidle():
    """Playwright marks networkidle DISCOURAGED, and Argo CD's application
    view holds a watch stream open so the network may never go idle.
    Regression guard: reintroducing it fails a successful login."""
    page = MagicMock()
    login_via_keycloak(page, "https://argocd.nebari.test/auth/login", "u", "p")
    page.wait_for_load_state.assert_not_called()


def test_login_returns_quietly_when_the_redirect_never_lands():
    """The journeys assert on where the browser ended up and explain each
    outcome; a raw Playwright timeout would replace those messages with a
    worse one."""
    page = MagicMock()
    page.wait_for_url.side_effect = PlaywrightTimeoutError("no redirect")
    login_via_keycloak(page, "https://argocd.nebari.test/auth/login", "u", "p")


def test_login_does_not_register_a_locator_handler():
    """add_locator_handler dismisses overlays; it does not redact trace
    snapshots. Registering one on the password field would be a no-op
    that reads like a security control, which is worse than nothing.
    Regression guard for that corrected design decision."""
    page = MagicMock()
    login_via_keycloak(page, "https://x", "u", "p")
    page.add_locator_handler.assert_not_called()


def test_access_denied_recognises_403():
    assert is_access_denied(403) is True


def test_access_denied_recognises_401():
    assert is_access_denied(401) is True


def test_access_denied_is_false_for_success():
    assert is_access_denied(200) is False


def test_login_waits_for_the_keycloak_form_before_filling_it():
    """Without this wait, a URL that does not redirect to Keycloak fails on
    the submit click after the full Playwright timeout, reporting a missing
    selector rather than the redirect that never happened."""
    page = MagicMock()
    login_via_keycloak(page, "https://x", "u", "p")
    page.wait_for_selector.assert_called_once()
    assert page.wait_for_selector.call_args.args[0] == KEYCLOAK_USERNAME_SELECTOR


def test_login_raises_a_diagnostic_error_naming_the_url_and_body_on_timeout():
    """A raw Playwright TimeoutError reads like the browser or TLS is
    broken. When the Keycloak form never appears, the actual page the
    browser landed on (and rendered) is far more useful, for example
    ArgoCD's own error body for a server-side OIDC discovery failure."""
    page = MagicMock()
    page.wait_for_selector.side_effect = PlaywrightTimeoutError("timeout")
    page.url = "https://argocd.nebari.local/auth/login"
    page.content.return_value = (
        'failed to query provider "https://keycloak.nebari.local/realms/nebari": '
        "dial tcp: lookup keycloak.nebari.local: server misbehaving"
    )
    with pytest.raises(RuntimeError) as excinfo:
        login_via_keycloak(page, "https://argocd.nebari.local/auth/login", "u", "p")
    message = str(excinfo.value)
    assert "https://argocd.nebari.local/auth/login" in message
    assert "failed to query provider" in message
    assert excinfo.value.__cause__ is not None


def test_login_error_truncates_a_very_long_page_body():
    page = MagicMock()
    page.wait_for_selector.side_effect = PlaywrightTimeoutError("timeout")
    page.url = "https://x"
    page.content.return_value = "x" * 100_000
    with pytest.raises(RuntimeError) as excinfo:
        login_via_keycloak(page, "https://x", "u", "p")
    assert len(str(excinfo.value)) < 10_000


def test_argocd_oidc_login_path_is_not_the_bare_host():
    """ArgoCD's own /login page renders its LOCAL username/password form and
    does not auto-redirect to the identity provider, so the journey must
    navigate to the path that starts the OIDC flow."""
    assert ARGOCD_OIDC_LOGIN_PATH.startswith("/")
    assert ARGOCD_OIDC_LOGIN_PATH != "/"


@pytest.mark.parametrize(
    "url,expected",
    [
        ("https://argocd.nebari.example/applications", "argocd.nebari.example"),
        ("https://keycloak.nebari.example/realms/nebari", "keycloak.nebari.example"),
        ("https://ARGOCD.Nebari.Example/", "argocd.nebari.example"),
        ("https://longhorn.nebari.example:8443/#/dashboard", "longhorn.nebari.example"),
        ("about:blank", ""),
        ("", ""),
    ],
)
def test_page_host_extracts_the_hostname(url, expected):
    assert page_host(url) == expected


# --- the OIDC round trip's completion signal -------------------------------
#
# login_via_keycloak used to end on wait_for_load_state("networkidle"),
# which Playwright's documentation marks DISCOURAGED and which cannot
# settle on a page holding a watch stream open (Argo CD's application
# view). The completion signal is "the browser is back on the application's
# host", judged by the same predicate the journeys assert with.


def test_returned_to_is_true_on_the_application_host():
    assert returned_to("argocd.nebari.test")("https://argocd.nebari.test/applications")


def test_returned_to_is_false_while_still_on_the_identity_provider():
    assert not returned_to("argocd.nebari.test")(
        "https://keycloak.nebari.test/realms/nebari/protocol/openid-connect/auth"
    )


def test_returned_to_ignores_path_and_query():
    check = returned_to("longhorn.nebari.test")
    assert check("https://longhorn.nebari.test/#/dashboard?tab=volume")


def test_returned_to_is_case_insensitive_on_both_sides():
    assert returned_to("ArgoCD.Nebari.Test")("https://argocd.NEBARI.test/applications")


def test_returned_to_is_false_for_a_url_with_no_host():
    assert not returned_to("argocd.nebari.test")("about:blank")


def test_returned_to_does_not_match_a_suffix_lookalike_host():
    """A substring or suffix match would accept
    argocd.nebari.test.attacker.example as "back on the app"."""
    check = returned_to("argocd.nebari.test")
    assert not check("https://argocd.nebari.test.attacker.example/")
    assert not check("https://not-argocd.nebari.test/")
