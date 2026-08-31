"""Keycloak admin REST actions.

TLS is verified against the trust anchor the fixtures derive. Journeys
prefer generated throwaway users over the admin credential wherever the
journey allows it, so failure artifacts carry as little as possible.

The admin password is never logged, printed, or included in an
exception message anywhere in this module, and it is excluded from the
dataclass repr (field(repr=False)) so it cannot leak into pytest
failure output, tracebacks, or the Playwright traces a later task
uploads as CI artifacts.
"""

import secrets
import time
import uuid
from collections.abc import Callable
from dataclasses import dataclass, field
from urllib.parse import urlsplit

import requests

from nebari_journeys import constants
from nebari_journeys.sweep import SweepResult

TOKEN_PATH = "/realms/master/protocol/openid-connect/token"
ADMIN_CLI = "admin-cli"
MASTER_ADMIN_USER = "admin"

# secrets.token_urlsafe, never random: this seeds a login credential for a
# live realm, so it must be a fresh, high-entropy value every call.
PASSWORD_BYTES = 24

REQUEST_TIMEOUT = 30
HTTP_UNAUTHORIZED = 401

# Keycloak bootstraps the MASTER realm with accessTokenLifespan = 60
# seconds -- hardcoded, not derived from the 300 second default new realms
# get: `realm.setAccessTokenLifespan(60)` in Keycloak's
# services/.../managers/ApplianceBootstrap.java. Nebari does not override
# it anywhere. This module authenticates against /realms/master (see
# TOKEN_PATH), so an admin token is valid for ONE MINUTE.
#
# A journey run that includes browser journeys routinely exceeds that:
# `test_identity.py` alone drives three Playwright logins after its first
# admin call, and the LAST thing the run does is delete_user in a fixture
# teardown. A token cached for the session would therefore expire
# mid-run and turn every later admin call into a 401 that reads like a
# platform authentication regression rather than a test harness bug --
# and a 401 on the teardown path leaks an enabled user into a live realm
# (see the sweep in `sweep_scratch_users`).
#
# Used only as the fallback when the token response omits `expires_in`;
# the value Keycloak actually reports is preferred.
DEFAULT_TOKEN_LIFETIME_SECONDS = 60

# Treat a token as expired this many seconds before it actually is, so a
# request issued just under the wire does not arrive at Keycloak already
# invalid. Must stay comfortably below DEFAULT_TOKEN_LIFETIME_SECONDS.
TOKEN_EXPIRY_MARGIN_SECONDS = 10

# Prefix for the throwaway realm users journeys create. Mirrors the role
# SCRATCH_NAMESPACE_PREFIX plays for namespaces: it is both what the
# generator builds from and the second guard the sweep checks, so the two
# cannot drift apart.
SCRATCH_USER_PREFIX = "journey-"

# Cap on the user listing the sweep asks Keycloak for. Deliberately not
# paginated: more than this many leftover scratch users means something is
# badly wrong, and an unattended sweep should not chase a runaway. The
# excess is simply left for the next run's sweep to pick up.
USER_SEARCH_LIMIT = 200


def generated_password() -> str:
    """A throwaway password for a scratch user. Never reused, never logged."""
    return secrets.token_urlsafe(PASSWORD_BYTES)


def redirect_hosts(client: dict) -> set[str]:
    """The hostnames an OIDC client's redirect URIs actually point at.

    Journeys check the client redirects to THIS platform, which means
    comparing hosts. A substring test against the joined URI list -- what
    this replaced -- passes on
    `https://argocd.nebari.test.somewhere-else.example/*`, and passes when
    the domain appears only in some unrelated client's URI, so it does not
    check what its name claims.

    A relative redirect URI has no host and contributes nothing. Wildcard
    paths (`/*`, the usual Keycloak form) parse fine; a wildcard in the
    HOST is deliberately left as-is so the caller sees it verbatim rather
    than having it silently normalised into a match.
    """
    hosts = set()
    for uri in client.get("redirectUris") or []:
        host = urlsplit(uri).hostname
        if host:
            hosts.add(host.lower())
    return hosts


def scratch_username() -> str:
    """A unique name for a throwaway realm user.

    Built from SCRATCH_USER_PREFIX, which is also the guard
    `sweep_scratch_users` re-checks before deleting anything, so the
    generator and the sweep cannot drift apart.
    """
    return f"{SCRATCH_USER_PREFIX}{uuid.uuid4().hex[:8]}"


def sweep_scratch_users(keycloak: "Keycloak") -> SweepResult:
    """Delete scratch realm users left behind by crashed runs.

    The mirror image of `k8s.sweep_stale_namespaces`, and it exists for the
    same reason: a run killed between `create_user` and its fixture
    teardown leaves an ENABLED user in a live realm with a password only
    the dead process knew. `test_longhorn_ui_admits_a_user_in_the_admins_group`
    puts that user in `longhorn-admins` before logging in, so the leak can
    be a privileged one.

    Two guards, as with the namespace sweep. Keycloak's username search is
    an INFIX match, so asking it for `journey-` also returns
    `prod-journey-admin`; the prefix is therefore re-checked here before
    anything is deleted, and a non-matching user is reported as skipped
    rather than silently dropped. A delete that fails does not abort the
    sweep -- the remaining leftovers still need clearing -- but it is
    recorded, because a user still sitting in the realm is exactly what
    the caller has to be told about.

    Never raises: a sweep that cannot list the realm reports itself as
    failed rather than aborting the run before a single journey has had a
    chance to produce its own, better diagnosis.
    """
    result = SweepResult()
    try:
        listing = keycloak.users(SCRATCH_USER_PREFIX)
    except (requests.exceptions.RequestException, KeyError, ValueError) as exc:
        # A cleanup step must never be the thing that breaks a run. If the
        # realm cannot even be listed, the journeys themselves will say so
        # in their own terms; report the sweep as having failed wholesale
        # and get out of the way.
        result.failed.append(f"<listing failed: {exc}>")
        return result
    for user in listing:
        username = user.get("username", "")
        if not username.startswith(SCRATCH_USER_PREFIX):
            result.skipped.append(username)
            continue
        try:
            keycloak.delete_user(user["id"])
        except (requests.exceptions.RequestException, KeyError):
            result.failed.append(username)
        else:
            result.deleted.append(username)
    return result


@dataclass
class Keycloak:
    base_url: str
    password: str = field(repr=False)
    verify: str | bool = True
    username: str = MASTER_ADMIN_USER
    _session: requests.Session | None = field(default=None, repr=False)
    _token: str | None = field(default=None, repr=False)
    _token_expires_at: float = field(default=0.0, repr=False)
    # Injectable monotonic clock, so the expiry logic is unit-testable
    # without sleeping through a real token lifetime.
    _now: Callable[[], float] = field(default=time.monotonic, repr=False)

    @classmethod
    def for_cluster(cls, cluster, domain: str, verify: str | bool | None) -> "Keycloak":
        return cls(
            base_url=f"https://keycloak.{domain}",
            password=cluster.keycloak_admin_password(),
            verify=verify if verify is not None else True,
        )

    @property
    def session(self) -> requests.Session:
        if self._session is None:
            self._session = requests.Session()
        return self._session

    def token(self) -> str:
        """A currently-valid admin access token, refetched when stale.

        Not cached for the session: see DEFAULT_TOKEN_LIFETIME_SECONDS for
        why one minute is the real budget.
        """
        if self._token is None or self._now() >= self._token_expires_at:
            self._fetch_token()
        return self._token

    def _fetch_token(self) -> None:
        issued_at = self._now()
        response = self.session.post(
            f"{self.base_url}{TOKEN_PATH}",
            data={
                "grant_type": "password",
                "client_id": ADMIN_CLI,
                "username": self.username,
                "password": self.password,
            },
            verify=self.verify,
            timeout=REQUEST_TIMEOUT,
        )
        response.raise_for_status()
        payload = response.json()
        self._token = payload["access_token"]
        lifetime = payload.get("expires_in") or DEFAULT_TOKEN_LIFETIME_SECONDS
        self._token_expires_at = (
            issued_at + float(lifetime) - TOKEN_EXPIRY_MARGIN_SECONDS
        )

    def _headers(self) -> dict:
        return {"Authorization": f"Bearer {self.token()}"}

    def _send(self, method: str, url: str, **kwargs):
        """Issue one admin call, refreshing the token on a 401 and retrying.

        Every admin request in this class goes through here. Proactive
        expiry (see `token`) handles the ordinary case; this handles the
        case no clock arithmetic can predict -- a token invalidated
        server-side by a session eviction or a Keycloak restart. Retried
        exactly once: a second 401 is a real authorization failure and
        must surface, not spin.
        """
        send = getattr(self.session, method)

        def issue():
            return send(
                url,
                headers=self._headers(),
                verify=self.verify,
                timeout=REQUEST_TIMEOUT,
                **kwargs,
            )

        response = issue()
        if response.status_code == HTTP_UNAUTHORIZED:
            # Drop the rejected token so _headers() fetches a new one.
            self._token = None
            response = issue()
        response.raise_for_status()
        return response

    def _admin_url(self, path: str) -> str:
        return f"{self.base_url}/admin/realms/{constants.REALM_NAME}{path}"

    def _get(self, path: str):
        return self._send("get", self._admin_url(path)).json()

    def realm(self) -> dict:
        return self._get("")

    def groups(self) -> list[dict]:
        return self._get("/groups")

    def clients(self) -> list[dict]:
        return self._get("/clients")

    def client(self, client_id: str) -> dict | None:
        for entry in self.clients():
            if entry.get("clientId") == client_id:
                return entry
        return None

    def realm_default_client_scopes(self) -> list[dict]:
        return self._get("/default-default-client-scopes")

    def group_id(self, name: str) -> str | None:
        for group in self.groups():
            if group.get("name") == name:
                return group.get("id")
        return None

    def users(self, search: str) -> list[dict]:
        """Realm users matching `search` (Keycloak's infix username match)."""
        return self._get(f"/users?username={search}&max={USER_SEARCH_LIMIT}")

    def create_user(self, username: str, password: str, groups=()) -> str:
        response = self._send(
            "post",
            self._admin_url("/users"),
            json={
                "username": username,
                "enabled": True,
                "emailVerified": True,
                "email": f"{username}@journeys.invalid",
                # firstName/lastName are required by Keycloak's declarative
                # user profile; without them the VERIFY_PROFILE required
                # action fires on first login and the user is dropped onto
                # an interactive account-completion form instead of
                # completing the OIDC redirect. Derived from the username
                # so the user stays identifiable in the realm.
                "firstName": "Journey",
                "lastName": username,
                # A scratch user exists only to prove the OIDC round trip
                # works. Any required action (UPDATE_PASSWORD,
                # CONFIGURE_TOTP, VERIFY_PROFILE, ...) turns that into an
                # interactive dead end that looks like an OIDC failure, so
                # this is set explicitly rather than relying on the realm
                # not having any default required actions configured.
                "requiredActions": [],
                "credentials": [
                    {"type": "password", "value": password, "temporary": False}
                ],
                "groups": list(groups),
            },
        )
        location = response.headers.get("Location")
        if location is None:
            raise RuntimeError(
                f"Keycloak did not return a Location header when creating user "
                f"{username!r}, so the new user id could not be determined"
            )
        # Keycloak's Location header may or may not carry a trailing slash
        # depending on version/proxy; strip it before taking the last segment.
        return location.rstrip("/").rsplit("/", 1)[-1]

    def delete_user(self, user_id: str) -> None:
        self._send("delete", self._admin_url(f"/users/{user_id}"))

    def add_user_to_group(self, user_id: str, group_name: str) -> None:
        group = self.group_id(group_name)
        if group is None:
            raise ValueError(f"group {group_name!r} not found in the realm")
        self._send("put", self._admin_url(f"/users/{user_id}/groups/{group}"))
