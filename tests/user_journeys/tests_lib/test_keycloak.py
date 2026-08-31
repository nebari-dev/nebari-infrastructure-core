from unittest.mock import MagicMock

import pytest
import requests

from nebari_journeys import keycloak as keycloak_module
from nebari_journeys.keycloak import (
    Keycloak,
    generated_password,
    redirect_hosts,
    scratch_username,
    sweep_scratch_users,
)
from nebari_journeys.sweep import SweepResult


def _kc(session=None):
    kc = Keycloak(
        base_url="https://keycloak.nebari.local",
        password="pw",
        verify="/tmp/ca.pem",
    )
    kc._session = session or MagicMock()
    kc._token = "tok"
    return kc


def test_generated_password_is_long_and_unique():
    passwords = {generated_password() for _ in range(20)}
    assert len(passwords) == 20
    assert all(len(p) >= 20 for p in passwords)


def test_token_request_targets_the_master_realm():
    session = MagicMock()
    session.post.return_value.json.return_value = {"access_token": "abc"}
    kc = _kc(session)
    kc._token = None
    assert kc.token() == "abc"
    url = session.post.call_args.args[0]
    assert url.endswith("/realms/master/protocol/openid-connect/token")


def test_requests_pass_the_trust_anchor_as_verify():
    session = MagicMock()
    kc = _kc(session)
    kc.realm()
    assert session.get.call_args.kwargs["verify"] == "/tmp/ca.pem"


def test_realm_url_uses_the_nebari_realm():
    session = MagicMock()
    kc = _kc(session)
    kc.realm()
    assert session.get.call_args.args[0].endswith("/admin/realms/nebari")


def test_create_user_posts_username_and_marks_enabled():
    session = MagicMock()
    session.post.return_value.headers = {
        "Location": "https://kc/admin/realms/nebari/users/user-123"
    }
    kc = _kc(session)
    user_id = kc.create_user("journey-abc", "pw123")
    body = session.post.call_args.kwargs["json"]
    assert body["username"] == "journey-abc"
    assert body["enabled"] is True
    assert user_id == "user-123"


def test_create_user_sets_a_non_empty_first_and_last_name():
    """Keycloak's declarative user profile requires firstName/lastName;
    without them VERIFY_PROFILE fires as a required action on first login
    and interrupts the OIDC round trip with an interactive form."""
    session = MagicMock()
    session.post.return_value.headers = {"Location": "https://kc/users/u1"}
    kc = _kc(session)
    kc.create_user("journey-abc", "pw123")
    body = session.post.call_args.kwargs["json"]
    assert body["firstName"]
    assert body["lastName"]


def test_create_user_clears_required_actions():
    """A scratch user must never be forced through an interactive
    account-completion flow, so requiredActions is sent explicitly empty
    rather than left to the realm's defaults."""
    session = MagicMock()
    session.post.return_value.headers = {"Location": "https://kc/users/u1"}
    kc = _kc(session)
    kc.create_user("journey-abc", "pw123")
    body = session.post.call_args.kwargs["json"]
    assert body["requiredActions"] == []


def test_create_user_sets_a_non_temporary_password():
    session = MagicMock()
    session.post.return_value.headers = {"Location": "https://kc/users/u1"}
    kc = _kc(session)
    kc.create_user("journey-abc", "pw123")
    credential = session.post.call_args.kwargs["json"]["credentials"][0]
    assert credential["value"] == "pw123"
    assert credential["temporary"] is False


def test_create_user_id_extraction_is_robust_to_a_trailing_slash():
    session = MagicMock()
    session.post.return_value.headers = {
        "Location": "https://kc/admin/realms/nebari/users/user-123/"
    }
    kc = _kc(session)
    user_id = kc.create_user("journey-abc", "pw123")
    assert user_id == "user-123"


def test_delete_user_targets_the_user_id():
    session = MagicMock()
    kc = _kc(session)
    kc.delete_user("user-123")
    assert session.delete.call_args.args[0].endswith("/users/user-123")


def test_client_returns_the_matching_client_id():
    session = MagicMock()
    session.get.return_value.json.return_value = [
        {"clientId": "argocd", "id": "uuid-1"},
        {"clientId": "longhorn", "id": "uuid-2"},
    ]
    kc = _kc(session)
    assert kc.client("longhorn")["id"] == "uuid-2"


def test_client_returns_none_when_absent():
    session = MagicMock()
    session.get.return_value.json.return_value = []
    assert _kc(session).client("missing") is None


def test_group_id_returns_the_matching_groups_id():
    session = MagicMock()
    session.get.return_value.json.return_value = [
        {"name": "everyone", "id": "group-1"},
        {"name": "longhorn-admins", "id": "group-2"},
    ]
    kc = _kc(session)
    assert kc.group_id("longhorn-admins") == "group-2"


def test_group_id_returns_none_when_absent():
    session = MagicMock()
    session.get.return_value.json.return_value = [
        {"name": "everyone", "id": "group-1"},
    ]
    kc = _kc(session)
    assert kc.group_id("missing") is None


def test_add_user_to_group_puts_to_the_resolved_group_url():
    session = MagicMock()
    session.get.return_value.json.return_value = [
        {"name": "longhorn-admins", "id": "group-2"},
    ]
    kc = _kc(session)
    kc.add_user_to_group("user-123", "longhorn-admins")
    url = session.put.call_args.args[0]
    assert url.endswith("/users/user-123/groups/group-2")
    assert session.put.call_args.kwargs["verify"] == "/tmp/ca.pem"


def test_add_user_to_group_raises_value_error_naming_the_group_when_absent():
    session = MagicMock()
    session.get.return_value.json.return_value = []
    kc = _kc(session)
    with pytest.raises(ValueError, match="missing-group"):
        kc.add_user_to_group("user-123", "missing-group")
    session.put.assert_not_called()


def test_repr_never_exposes_the_password():
    kc = Keycloak(
        base_url="https://keycloak.nebari.local",
        password="EXTREMELY-SECRET-VALUE",
        verify="/tmp/ca.pem",
    )
    representation = repr(kc)
    assert "EXTREMELY-SECRET-VALUE" not in representation
    assert "password=" not in representation


# --- token lifetime and refresh -------------------------------------------
#
# Keycloak's master realm is bootstrapped with accessTokenLifespan = 60
# seconds, hardcoded in ApplianceBootstrap.java, and Nebari does not
# override it. The suite authenticates against /realms/master, so an admin
# token is valid for one minute -- shorter than a run that includes browser
# journeys. These tests pin the refresh behaviour that makes that survivable.


def _token_response(access_token="fresh", expires_in=60):
    response = MagicMock()
    response.json.return_value = {
        "access_token": access_token,
        "expires_in": expires_in,
    }
    return response


def test_token_is_reused_while_it_is_still_valid():
    session = MagicMock()
    session.post.return_value = _token_response("abc")
    kc = _kc(session)
    kc._token = None
    assert kc.token() == "abc"
    assert kc.token() == "abc"
    assert session.post.call_count == 1


def test_token_is_refetched_once_it_has_expired():
    """The whole point: a cached token outliving its 60 second lifespan
    turns every later admin call into a 401 that reads like a platform
    auth regression."""
    session = MagicMock()
    session.post.side_effect = [_token_response("first"), _token_response("second")]
    kc = _kc(session)
    kc._token = None

    clock = [0.0]
    kc._now = lambda: clock[0]

    assert kc.token() == "first"
    clock[0] = 100.0
    assert kc.token() == "second"
    assert session.post.call_count == 2


def test_token_expiry_uses_a_margin_so_it_never_expires_in_flight():
    """A token fetched with 60 seconds left must be treated as expired
    before it actually is, or a call issued at t=59.9 arrives at Keycloak
    already invalid."""
    session = MagicMock()
    session.post.side_effect = [_token_response("first"), _token_response("second")]
    kc = _kc(session)
    kc._token = None

    clock = [0.0]
    kc._now = lambda: clock[0]

    assert kc.token() == "first"
    # Still 5 seconds of real validity left, but inside the margin.
    clock[0] = 55.0
    assert kc.token() == "second"


def test_token_falls_back_to_the_master_realm_lifetime_when_expires_in_is_absent():
    session = MagicMock()
    session.post.return_value.json.return_value = {"access_token": "abc"}
    kc = _kc(session)
    kc._token = None
    kc._now = lambda: 0.0
    kc.token()
    assert kc._token_expires_at == pytest.approx(
        60 - keycloak_module.TOKEN_EXPIRY_MARGIN_SECONDS
    )


def test_a_401_refreshes_the_token_and_retries_the_call_once():
    """Belt and braces alongside proactive expiry: a token can also be
    invalidated server-side (session eviction, a Keycloak restart), which
    no clock arithmetic predicts."""
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    unauthorized = MagicMock(status_code=401)
    ok = MagicMock(status_code=200)
    ok.json.return_value = {"realm": "nebari"}
    session.get.side_effect = [unauthorized, ok]

    kc = _kc(session)
    assert kc.realm() == {"realm": "nebari"}
    assert session.get.call_count == 2
    # The retry must carry the NEW token, not the rejected one.
    assert session.get.call_args.kwargs["headers"]["Authorization"] == "Bearer fresh"
    unauthorized.raise_for_status.assert_not_called()


def test_a_401_is_retried_only_once_then_raised():
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    unauthorized = MagicMock(status_code=401)
    unauthorized.raise_for_status.side_effect = requests.exceptions.HTTPError("401")
    session.get.side_effect = [unauthorized, unauthorized]

    kc = _kc(session)
    with pytest.raises(requests.exceptions.HTTPError):
        kc.realm()
    assert session.get.call_count == 2


def test_a_401_on_a_mutating_call_is_also_retried():
    """create_user and delete_user run at the very end of a run, which is
    exactly when the token is most likely to have expired -- a failed
    delete_user leaks an enabled user into a live realm."""
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    unauthorized = MagicMock(status_code=401)
    ok = MagicMock(status_code=204)
    session.delete.side_effect = [unauthorized, ok]

    kc = _kc(session)
    kc.delete_user("user-123")
    assert session.delete.call_count == 2


# --- scratch user sweep ----------------------------------------------------
#
# Namespaces created by a crashed run are swept at session start. Scratch
# realm users were not, which made the cleanup asymmetric: a run killed
# between create_user and its teardown left an ENABLED user in a live realm,
# and the admits journey adds that user to longhorn-admins before logging in,
# so the leak can be a privileged one.


def _user(username, uid=None):
    return {"username": username, "id": uid or f"id-{username}"}


def test_sweep_deletes_users_matching_the_scratch_prefix():
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    listing = MagicMock(status_code=200)
    listing.json.return_value = [_user("journey-aaaa"), _user("journey-bbbb")]
    session.get.return_value = listing
    session.delete.return_value = MagicMock(status_code=204)

    kc = _kc(session)
    result = sweep_scratch_users(kc)

    assert sorted(result.deleted) == ["journey-aaaa", "journey-bbbb"]
    assert result.skipped == []
    assert result.failed == []
    assert session.delete.call_count == 2


def test_sweep_never_deletes_a_user_outside_the_scratch_prefix():
    """Keycloak's username search is an INFIX match, so asking for
    "journey-" also returns "prod-journey-admin". The prefix is re-checked
    client side before anything is deleted: this sweep runs unattended
    against a realm that may be production."""
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    listing = MagicMock(status_code=200)
    listing.json.return_value = [
        _user("journey-aaaa"),
        _user("prod-journey-admin"),
        _user("real-user"),
    ]
    session.get.return_value = listing
    session.delete.return_value = MagicMock(status_code=204)

    kc = _kc(session)
    result = sweep_scratch_users(kc)

    assert result.deleted == ["journey-aaaa"]
    assert sorted(result.skipped) == ["prod-journey-admin", "real-user"]
    assert session.delete.call_count == 1


def test_sweep_records_a_failed_delete_and_keeps_going():
    """One undeletable user must not stop the sweep from clearing the rest,
    and must not vanish: what is still in the realm is the whole point."""
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    listing = MagicMock(status_code=200)
    listing.json.return_value = [_user("journey-aaaa"), _user("journey-bbbb")]
    session.get.return_value = listing
    boom = MagicMock(status_code=500)
    boom.raise_for_status.side_effect = requests.exceptions.HTTPError("boom")
    session.delete.side_effect = [boom, MagicMock(status_code=204)]

    kc = _kc(session)
    result = sweep_scratch_users(kc)

    assert result.deleted == ["journey-bbbb"]
    assert result.failed == ["journey-aaaa"]


def test_sweep_reports_nothing_when_the_realm_is_clean():
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    listing = MagicMock(status_code=200)
    listing.json.return_value = []
    session.get.return_value = listing

    kc = _kc(session)
    result = sweep_scratch_users(kc)

    assert result == SweepResult()
    session.delete.assert_not_called()


def test_scratch_username_is_built_from_the_swept_prefix():
    """The generator and the sweep's guard must build from one constant, or
    a rename leaves users the sweep will never look at."""
    assert scratch_username().startswith(keycloak_module.SCRATCH_USER_PREFIX)
    assert scratch_username() != scratch_username()


# --- redirect URI hosts ----------------------------------------------------


@pytest.mark.parametrize(
    "uris,expected",
    [
        (["https://argocd.nebari.test/auth/callback"], {"argocd.nebari.test"}),
        # Keycloak's usual wildcard-path form.
        (["https://argocd.nebari.test/*"], {"argocd.nebari.test"}),
        (["HTTPS://ArgoCD.Nebari.Test/*"], {"argocd.nebari.test"}),
        # Relative URIs have no host and contribute nothing.
        (["/auth/callback"], set()),
        ([], set()),
        (
            ["https://a.nebari.test/*", "https://b.nebari.test/*"],
            {"a.nebari.test", "b.nebari.test"},
        ),
    ],
)
def test_redirect_hosts_extracts_hostnames(uris, expected):
    assert redirect_hosts({"redirectUris": uris}) == expected


def test_redirect_hosts_handles_a_client_with_no_redirect_uris_key():
    assert redirect_hosts({}) == set()


def test_redirect_hosts_does_not_conflate_a_lookalike_domain():
    """The substring check this replaced accepted exactly this URI as proof
    that the client redirects to nebari.test."""
    hosts = redirect_hosts(
        {"redirectUris": ["https://argocd.nebari.test.somewhere-else.example/*"]}
    )
    assert hosts == {"argocd.nebari.test.somewhere-else.example"}
    assert "argocd.nebari.test" not in hosts


def test_sweep_never_raises_when_the_realm_cannot_be_listed():
    """A cleanup step must not be what breaks the run: the journeys give a
    far better diagnosis of an unreachable or unconfigured realm than a
    traceback out of a fixture does."""
    session = MagicMock()
    session.post.return_value = _token_response("fresh")
    missing = MagicMock(status_code=404)
    missing.raise_for_status.side_effect = requests.exceptions.HTTPError("404")
    session.get.return_value = missing

    kc = _kc(session)
    result = sweep_scratch_users(kc)

    assert result.deleted == []
    assert len(result.failed) == 1
    assert "listing failed" in result.failed[0]
    session.delete.assert_not_called()
