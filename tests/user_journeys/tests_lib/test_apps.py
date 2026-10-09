from unittest.mock import MagicMock

import pytest

from nebari_journeys import apps


def nebariapp(auth=None, hostname="Nebi.Example.Test", conditions=()):
    return {
        "metadata": {"namespace": "nebi", "name": "nebi-app"},
        "spec": {"hostname": hostname, **({"auth": auth} if auth is not None else {})},
        "status": {"conditions": list(conditions)},
    }


@pytest.mark.parametrize(
    ("auth", "expected"),
    [
        (None, False),
        ({}, False),
        ({"enabled": False}, False),
        ({"enabled": False, "enforceAtGateway": True}, False),
        # Absent means true: the operator's default.
        ({"enabled": True}, True),
        ({"enabled": True, "enforceAtGateway": True}, True),
        ({"enabled": True, "enforceAtGateway": False}, False),
    ],
)
def test_gateway_enforced_follows_the_operator_default(auth, expected):
    assert apps.gateway_enforced(auth) is expected


def test_nebariapp_reads_host_auth_and_conditions():
    app = apps.NebariApp.from_object(
        nebariapp(
            auth={"enabled": True},
            conditions=[{"type": "Ready", "status": "True"}],
        )
    )
    assert app.ref == "nebi/nebi-app"
    assert app.hostname == "nebi.example.test"
    assert app.auth_enabled and app.enforce_at_gateway
    assert app.condition_status("Ready") == "True"
    assert app.condition_status("AuthReady") is None


def test_nebariapp_tolerates_a_missing_spec_and_status():
    app = apps.NebariApp.from_object({"metadata": {"namespace": "a", "name": "b"}})
    assert app.hostname == ""
    assert not app.auth_enabled and not app.enforce_at_gateway
    assert app.conditions == {}


def owned(kind="NebariApp", name="nebi-app", controller=True):
    return {
        "metadata": {
            "ownerReferences": [{"kind": kind, "name": name, "controller": controller}]
        }
    }


def test_controlled_by_matches_the_controller_owner():
    assert apps.controlled_by(owned(), "NebariApp", "nebi-app")


@pytest.mark.parametrize(
    "obj",
    [
        owned(name="other-app"),
        owned(kind="Deployment"),
        owned(controller=False),
        {"metadata": {}},
    ],
)
def test_controlled_by_rejects_other_owners(obj):
    assert not apps.controlled_by(obj, "NebariApp", "nebi-app")


def test_acceptance_collects_the_accepted_condition_of_every_ancestor():
    policy = {
        "status": {
            "ancestors": [
                {"conditions": [{"type": "Accepted", "status": "True"}]},
                {
                    "conditions": [
                        {"type": "ResolvedRefs", "status": "True"},
                        {"type": "Accepted", "status": "False", "reason": "Invalid"},
                    ]
                },
            ]
        }
    }
    assert [c["status"] for c in apps.acceptance(policy)] == ["True", "False"]


def test_acceptance_is_empty_before_the_gateway_reports():
    assert apps.acceptance({}) == []
    assert apps.acceptance({"status": {"ancestors": []}}) == []


def test_oidc_provider_tolerates_a_policy_without_oidc():
    assert apps.oidc_provider({"spec": {}}) == {}
    assert apps.oidc_provider({"spec": {"oidc": {"provider": {"issuer": "x"}}}}) == {
        "issuer": "x"
    }


def test_host_of_lowercases_and_handles_bare_strings():
    assert (
        apps.host_of("https://Keycloak.Example.Test/realms/nebari")
        == "keycloak.example.test"
    )
    assert apps.host_of("") == ""


def test_destination_namespace_reads_the_application_spec():
    cluster = MagicMock()
    cluster.applications.return_value = [
        {"metadata": {"name": "other"}, "spec": {"destination": {"namespace": "x"}}},
        {
            "metadata": {"name": "data-science-pack"},
            "spec": {"destination": {"namespace": "jupyterhub"}},
        },
    ]
    assert apps.destination_namespace(cluster, "data-science-pack") == "jupyterhub"
    assert apps.destination_namespace(cluster, "missing") is None


def test_wait_for_app_ready_returns_as_soon_as_ready(monkeypatch):
    ready = apps.NebariApp.from_object(
        nebariapp(
            auth={"enabled": True}, conditions=[{"type": "Ready", "status": "True"}]
        )
    )
    monkeypatch.setattr(apps, "nebari_apps_in", lambda cluster, ns: [ready])
    monkeypatch.setattr(apps.time, "sleep", lambda s: pytest.fail("should not wait"))
    pending = apps.NebariApp.from_object(nebariapp(auth={"enabled": True}))
    assert apps.wait_for_app_ready(MagicMock(), pending) is ready


def test_wait_for_app_ready_gives_up_with_the_last_state(monkeypatch):
    pending = apps.NebariApp.from_object(nebariapp(auth={"enabled": True}))
    clock = iter([0.0, 0.0, apps.APP_READY_TIMEOUT + 1])
    monkeypatch.setattr(apps, "nebari_apps_in", lambda cluster, ns: [pending])
    monkeypatch.setattr(apps.time, "monotonic", lambda: next(clock))
    monkeypatch.setattr(apps.time, "sleep", lambda s: None)
    assert (
        apps.wait_for_app_ready(MagicMock(), pending).condition_status("Ready") is None
    )


def _jwt(claims: dict) -> str:
    import base64
    import json

    def enc(obj):
        return base64.urlsafe_b64encode(json.dumps(obj).encode()).rstrip(b"=").decode()

    return f"{enc({'alg': 'RS256'})}.{enc(claims)}.c2lnbmF0dXJl"


def test_jwt_claims_reads_the_payload():
    assert apps.jwt_claims(_jwt({"aud": "admin-cli", "exp": 1}))["aud"] == "admin-cli"


def test_with_audience_rewrites_the_claim_but_keeps_the_old_signature():
    token = _jwt({"aud": "admin-cli", "sub": "u"})
    forged = apps.with_audience(token, "nebi")
    assert apps.jwt_claims(forged) == {"aud": "nebi", "sub": "u"}
    assert forged.split(".")[0] == token.split(".")[0]
    assert forged.split(".")[2] == token.split(".")[2]
    assert forged != token


def test_cookies_for_host_matches_the_exact_host_only():
    context = MagicMock()
    context.cookies.return_value = [
        {"name": "a", "value": "1", "domain": "nebi.example.test"},
        {"name": "b", "value": "2", "domain": ".nebi.example.test"},
        {"name": "c", "value": "3", "domain": "keycloak.example.test"},
    ]
    assert apps.cookies_for_host(context, "Nebi.Example.Test") == {"a": "1", "b": "2"}


def test_session_cookie_names_ignore_the_login_flow_cookies():
    cookies = [
        "AccessToken-1",
        "IdToken-1",
        "OauthHMAC-1",
        "RefreshToken-1",
        "OauthExpires-1",
        "CodeVerifier-1",
        "OauthNonce-1",
    ]
    assert apps.session_cookie_names(cookies) == [
        "AccessToken-1",
        "IdToken-1",
        "OauthHMAC-1",
        "RefreshToken-1",
    ]


def test_policy_paths_come_from_the_oidc_spec():
    policy = {
        "spec": {
            "oidc": {
                "logoutPath": "/logout",
                "redirectURL": "https://nebi.example.test/oauth2/callback",
            }
        }
    }
    assert apps.logout_path(policy) == "/logout"
    assert apps.callback_path(policy) == "/oauth2/callback"
    assert apps.logout_path({}) is None
    assert apps.callback_path({}) == "/oauth2/callback"


@pytest.mark.parametrize(
    ("port", "expected"),
    [
        (80, "http://svc.nebi.svc.cluster.local"),
        (0, "http://svc.nebi.svc.cluster.local"),
        (8460, "http://svc.nebi.svc.cluster.local:8460"),
    ],
)
def test_in_cluster_url_omits_the_default_port(port, expected):
    obj = nebariapp(auth={"enabled": True})
    obj["spec"]["service"] = {"name": "svc", "port": port}
    assert apps.NebariApp.from_object(obj).in_cluster_url == expected
