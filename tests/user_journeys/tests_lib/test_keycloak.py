from unittest.mock import MagicMock

from nebari_journeys.keycloak import Keycloak, generated_password


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


def test_repr_never_exposes_the_password():
    kc = Keycloak(
        base_url="https://keycloak.nebari.local",
        password="EXTREMELY-SECRET-VALUE",
        verify="/tmp/ca.pem",
    )
    representation = repr(kc)
    assert "EXTREMELY-SECRET-VALUE" not in representation
    assert "password=" not in representation
