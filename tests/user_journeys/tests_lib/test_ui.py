from unittest.mock import MagicMock

import pytest

from nebari_journeys.ui import (
    ARGOCD_OIDC_LOGIN_PATH,
    KEYCLOAK_PASSWORD_SELECTOR,
    KEYCLOAK_SUBMIT_SELECTOR,
    KEYCLOAK_USERNAME_SELECTOR,
    is_access_denied,
    login_via_keycloak,
    page_host,
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


def test_login_waits_for_navigation_after_submitting():
    page = MagicMock()
    login_via_keycloak(page, "https://x", "u", "p")
    page.wait_for_load_state.assert_called_once()


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
