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
from dataclasses import dataclass, field

import requests

from nebari_journeys import constants

TOKEN_PATH = "/realms/master/protocol/openid-connect/token"
ADMIN_CLI = "admin-cli"
MASTER_ADMIN_USER = "admin"

# secrets.token_urlsafe, never random: this seeds a login credential for a
# live realm, so it must be a fresh, high-entropy value every call.
PASSWORD_BYTES = 24


def generated_password() -> str:
    """A throwaway password for a scratch user. Never reused, never logged."""
    return secrets.token_urlsafe(PASSWORD_BYTES)


@dataclass
class Keycloak:
    base_url: str
    password: str = field(repr=False)
    verify: str | bool = True
    username: str = MASTER_ADMIN_USER
    _session: requests.Session | None = field(default=None, repr=False)
    _token: str | None = field(default=None, repr=False)

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
        if self._token is None:
            response = self.session.post(
                f"{self.base_url}{TOKEN_PATH}",
                data={
                    "grant_type": "password",
                    "client_id": ADMIN_CLI,
                    "username": self.username,
                    "password": self.password,
                },
                verify=self.verify,
                timeout=30,
            )
            response.raise_for_status()
            self._token = response.json()["access_token"]
        return self._token

    def _headers(self) -> dict:
        return {"Authorization": f"Bearer {self.token()}"}

    def _get(self, path: str):
        response = self.session.get(
            f"{self.base_url}/admin/realms/{constants.REALM_NAME}{path}",
            headers=self._headers(),
            verify=self.verify,
            timeout=30,
        )
        response.raise_for_status()
        return response.json()

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

    def create_user(self, username: str, password: str, groups=()) -> str:
        response = self.session.post(
            f"{self.base_url}/admin/realms/{constants.REALM_NAME}/users",
            headers=self._headers(),
            json={
                "username": username,
                "enabled": True,
                "emailVerified": True,
                "email": f"{username}@journeys.invalid",
                "credentials": [
                    {"type": "password", "value": password, "temporary": False}
                ],
                "groups": list(groups),
            },
            verify=self.verify,
            timeout=30,
        )
        response.raise_for_status()
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
        response = self.session.delete(
            f"{self.base_url}/admin/realms/{constants.REALM_NAME}/users/{user_id}",
            headers=self._headers(),
            verify=self.verify,
            timeout=30,
        )
        response.raise_for_status()

    def add_user_to_group(self, user_id: str, group_name: str) -> None:
        group = self.group_id(group_name)
        if group is None:
            raise ValueError(f"group {group_name!r} not found in the realm")
        response = self.session.put(
            f"{self.base_url}/admin/realms/{constants.REALM_NAME}"
            f"/users/{user_id}/groups/{group}",
            headers=self._headers(),
            verify=self.verify,
            timeout=30,
        )
        response.raise_for_status()
