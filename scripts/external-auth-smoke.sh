#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"
NEBARI_ROOT="${WORKSPACE_ROOT}/nebari"
NEBI_ROOT="${WORKSPACE_ROOT}/nebi"

KEYCLOAK_IMAGE="${KEYCLOAK_IMAGE:-quay.io/keycloak/keycloak:24.0}"
KEYCLOAK_CONTAINER="${KEYCLOAK_CONTAINER:-kc-oauth-smoke}"
KEYCLOAK_PORT="${KEYCLOAK_PORT:-28080}"
BROKER_PORT="${BROKER_PORT:-18765}"
MOCK_BROKER_PORT="${MOCK_BROKER_PORT:-18766}"
NEBI_PORT="${NEBI_PORT:-18460}"
NEBI_LINKED_PORT="${NEBI_LINKED_PORT:-18461}"
KEYCLOAK_ADMIN_USER="${KEYCLOAK_ADMIN_USER:-admin}"
KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD:-admin}"
KEYCLOAK_REALM="${KEYCLOAK_REALM:-nebari}"
TEST_SUBJECT="${TEST_SUBJECT:-linked-user}"

NEBARI_VENV_PYTHON="${NEBARI_VENV_PYTHON:-/workspace/.venvs/nebari/bin/python}"
GO_BIN="${GO_BIN:-/workspace/.tooling/go1.26.1/go/bin/go}"
SMOKE_OUT="/tmp/external-auth-smoke.out"
NEBI_PIDS=()
BROKER_PIDS=()

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "missing required command: $cmd" >&2
    exit 1
  }
}

cleanup() {
  for pid in "${NEBI_PIDS[@]:-}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      kill "${pid}" || true
      wait "${pid}" 2>/dev/null || true
    fi
  done
  for pid in "${BROKER_PIDS[@]:-}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      kill "${pid}" || true
      wait "${pid}" 2>/dev/null || true
    fi
  done

  docker rm -f "${KEYCLOAK_CONTAINER}" >/dev/null 2>&1 || true
  rm -f "${SMOKE_OUT}" /tmp/nebari-external-auth-smoke.log /tmp/nebi-external-auth-smoke.log /tmp/mock-broker-smoke.log
}
trap cleanup EXIT

wait_for_http() {
  local url="$1"
  local header="${2:-}"
  local attempts="${3:-40}"
  for _ in $(seq 1 "${attempts}"); do
    if [[ -n "${header}" ]]; then
      if curl -fsS "${url}" -H "${header}" >/dev/null 2>&1; then
        return 0
      fi
    else
      if curl -fsS "${url}" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 1
  done
  echo "timeout waiting for ${url}" >&2
  exit 1
}

start_nebi() {
  local broker_url="$1"
  local nebi_port="$2"
  local logfile="/tmp/nebi-external-auth-smoke-${nebi_port}.log"
  echo "[smoke] starting nebi on :${nebi_port} -> ${broker_url}"
  (
    cd "${NEBI_ROOT}"
    NEBI_MODE=local \
    NEBI_SERVER_PORT="${nebi_port}" \
    NEBI_AUTH_EXTERNAL_AUTH_ENABLED=true \
    NEBI_AUTH_EXTERNAL_AUTH_URL="${broker_url}" \
    NEBI_AUTH_EXTERNAL_AUTH_CLIENT_ID="nebi" \
    NEBI_AUTH_EXTERNAL_AUTH_TIMEOUT_SECONDS=10 \
    "${GO_BIN}" run ./cmd/nebi serve --mode server --port "${nebi_port}" >"${logfile}" 2>&1
  ) &
  NEBI_PIDS+=("$!")
  wait_for_http "http://127.0.0.1:${nebi_port}/api/v1/health"
}

require_cmd docker
require_cmd curl
require_cmd openssl

if [[ ! -x "${NEBARI_VENV_PYTHON}" ]]; then
  echo "nebari python not found at ${NEBARI_VENV_PYTHON}" >&2
  exit 1
fi

if [[ ! -x "${GO_BIN}" ]]; then
  echo "go binary not found at ${GO_BIN}" >&2
  exit 1
fi

if [[ ! -d "${NEBARI_ROOT}" ]]; then
  echo "expected nebari repo at ${NEBARI_ROOT}" >&2
  exit 1
fi

if [[ ! -d "${NEBI_ROOT}" ]]; then
  echo "expected nebi repo at ${NEBI_ROOT}" >&2
  exit 1
fi

echo "[smoke] starting keycloak ${KEYCLOAK_IMAGE} on :${KEYCLOAK_PORT}"
docker rm -f "${KEYCLOAK_CONTAINER}" >/dev/null 2>&1 || true
docker run -d --name "${KEYCLOAK_CONTAINER}" \
  -p "${KEYCLOAK_PORT}:8080" \
  -e KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN_USER}" \
  -e KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD}" \
  "${KEYCLOAK_IMAGE}" start-dev >/dev/null

wait_for_http "http://127.0.0.1:${KEYCLOAK_PORT}/" "" 120

admin_token="$({
  curl -fsS -X POST "http://127.0.0.1:${KEYCLOAK_PORT}/realms/master/protocol/openid-connect/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "client_id=admin-cli" \
    --data-urlencode "grant_type=password" \
    --data-urlencode "username=${KEYCLOAK_ADMIN_USER}" \
    --data-urlencode "password=${KEYCLOAK_ADMIN_PASSWORD}"
} | ${NEBARI_VENV_PYTHON} -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"

curl -sS -o /dev/null -w "%{http_code}" \
  -X POST "http://127.0.0.1:${KEYCLOAK_PORT}/admin/realms" \
  -H "Authorization: Bearer ${admin_token}" \
  -H "Content-Type: application/json" \
  -d "{\"realm\":\"${KEYCLOAK_REALM}\",\"enabled\":true}" | grep -Eq '201|409'

curl -sS -o /dev/null -w "%{http_code}" \
  -X POST "http://127.0.0.1:${KEYCLOAK_PORT}/admin/realms/${KEYCLOAK_REALM}/users" \
  -H "Authorization: Bearer ${admin_token}" \
  -H "Content-Type: application/json" \
  -d "{\"id\":\"${TEST_SUBJECT}\",\"username\":\"${TEST_SUBJECT}\",\"enabled\":true}" | grep -Eq '201|409'

echo "[smoke] starting nebari broker on :${BROKER_PORT}"
(
  cd "${NEBARI_ROOT}"
  KEYCLOAK_SERVER_URL="http://127.0.0.1:${KEYCLOAK_PORT}/" \
  KEYCLOAK_REALM="${KEYCLOAK_REALM}" \
  KEYCLOAK_ADMIN_USERNAME="${KEYCLOAK_ADMIN_USER}" \
  KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD}" \
  PYTHONPATH="${NEBARI_ROOT}/src" \
  "${NEBARI_VENV_PYTHON}" -m nebari keycloak serve-external-auth-broker \
    -c tests/tests_unit/cli_validate/local.happy.github.yaml \
    --host 127.0.0.1 --port "${BROKER_PORT}" >/tmp/nebari-external-auth-smoke.log 2>&1
) &
BROKER_PIDS+=("$!")
wait_for_http \
  "http://127.0.0.1:${BROKER_PORT}/external-auth/providers" \
  "Authorization: Bearer fake.token.sig"

start_nebi "http://127.0.0.1:${BROKER_PORT}" "${NEBI_PORT}"

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
HEADER=$(printf '%s' '{"alg":"none","typ":"JWT"}' | b64url)
PAYLOAD=$(printf '{"sub":"%s","iss":"external-auth-smoke","exp":4102444800}' "${TEST_SUBJECT}" | b64url)
TOKEN="${HEADER}.${PAYLOAD}.x"

assert_response() {
  local method="$1"
  local url="$2"
  local expected_status="$3"
  local body_match="$4"
  local body=""
  local status=""

  if [[ "$method" == "POST" ]]; then
    status=$(curl -sS -o "${SMOKE_OUT}" -w "%{http_code}" \
      -X POST -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
      -d '{"requested_scopes":["repo"],"redirect_uri":"http://localhost/callback","state":"smoke"}' \
      "${url}")
  else
    status=$(curl -sS -o "${SMOKE_OUT}" -w "%{http_code}" \
      -X "${method}" -H "Authorization: Bearer ${TOKEN}" "${url}")
  fi

  body=$(cat "${SMOKE_OUT}")
  if [[ "${status}" != "${expected_status}" ]]; then
    echo "[smoke] failed ${method} ${url}: status=${status}, expected=${expected_status}" >&2
    echo "[smoke] body: ${body}" >&2
    exit 1
  fi

  if [[ -n "${body_match}" ]] && ! grep -Fq "${body_match}" "${SMOKE_OUT}"; then
    echo "[smoke] failed ${method} ${url}: missing body token '${body_match}'" >&2
    echo "[smoke] body: ${body}" >&2
    exit 1
  fi

  echo "[smoke] ok ${method} ${url} -> ${status}"
}

echo "[smoke] scenario: real keycloak unlinked"
assert_response GET "http://127.0.0.1:${NEBI_PORT}/api/v1/external-auth/providers" 200 '"providers"'
assert_response POST "http://127.0.0.1:${NEBI_PORT}/api/v1/external-auth/github/login" 200 '"redirect_required"'
assert_response GET "http://127.0.0.1:${NEBI_PORT}/api/v1/external-auth/github/token" 401 '"reauth_required"'
assert_response DELETE "http://127.0.0.1:${NEBI_PORT}/api/v1/external-auth/github/link" 200 '"unlinked"'

echo "[smoke] scenario: mocked linked token_valid"
(
  "${NEBARI_VENV_PYTHON}" - "${MOCK_BROKER_PORT}" <<'PY' >/tmp/mock-broker-smoke.log 2>&1
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

port = int(sys.argv[1])

class Handler(BaseHTTPRequestHandler):
    def _json(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _auth(self):
        return self.headers.get("Authorization", "").startswith("Bearer ")

    def do_GET(self):  # noqa: N802
        if not self._auth():
            self._json(401, {"error": {"code": "internal_error", "message": "missing authorization"}})
            return
        if self.path.startswith("/external-auth/providers"):
            self._json(200, {"providers": [{"id": "github", "display_name": "GitHub", "link_status": "linked", "allowed_scopes": ["repo"], "granted_scopes": ["repo"], "last_linked_at": "2026-01-01T00:00:00Z"}]})
            return
        if self.path.startswith("/external-auth/github/token"):
            self._json(200, {"status": "token_valid", "access_token": "mock-linked-token", "token_type": "Bearer", "expires_in": 300, "scope": "repo", "provider_user": "mock-user"})
            return
        self._json(404, {"error": {"code": "link_not_found", "message": "route not found"}})

    def do_POST(self):  # noqa: N802
        if not self._auth():
            self._json(401, {"error": {"code": "internal_error", "message": "missing authorization"}})
            return
        if self.path == "/external-auth/github/login":
            self._json(200, {"status": "redirect_required", "auth_url": "https://example/broker/github/login"})
            return
        self._json(404, {"error": {"code": "link_not_found", "message": "route not found"}})

    def do_DELETE(self):  # noqa: N802
        if not self._auth():
            self._json(401, {"error": {"code": "internal_error", "message": "missing authorization"}})
            return
        if self.path == "/external-auth/github/link":
            self._json(200, {"status": "unlinked"})
            return
        self._json(404, {"error": {"code": "link_not_found", "message": "route not found"}})

    def log_message(self, fmt, *args):  # noqa: A003
        return

ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
) &
BROKER_PIDS+=("$!")

wait_for_http \
  "http://127.0.0.1:${MOCK_BROKER_PORT}/external-auth/providers" \
  "Authorization: Bearer fake.token.sig"
start_nebi "http://127.0.0.1:${MOCK_BROKER_PORT}" "${NEBI_LINKED_PORT}"

assert_response GET "http://127.0.0.1:${NEBI_LINKED_PORT}/api/v1/external-auth/providers" 200 '"linked"'
assert_response GET "http://127.0.0.1:${NEBI_LINKED_PORT}/api/v1/external-auth/github/token" 200 '"token_valid"'
assert_response GET "http://127.0.0.1:${NEBI_LINKED_PORT}/api/v1/external-auth/github/token" 200 '"mock-linked-token"'

echo "[smoke] external auth contract checks passed (unlinked + token_valid)"
