# External Auth Smoke Runbook

This runbook covers local troubleshooting for `scripts/external-auth-smoke.sh`.

## Run

```bash
make external-auth-smoke
# or
bash ./scripts/external-auth-smoke.sh
```

Expected final line:

```text
[smoke] external auth contract checks passed (unlinked + token_valid)
```

## Failure Triage

### Port conflicts (28080, 18765, 18766, 18460, 18461)

Symptom:
- Bind/listen errors or startup timeout.

Fix:

```bash
netstat -ltn 2>/dev/null | egrep ':28080|:18765|:18766|:18460|:18461' || true
docker rm -f kc-oauth-smoke kc-oauth-test >/dev/null 2>&1 || true
pkill -f 'serve-external-auth-broker' || true
pkill -f 'go run ./cmd/nebi serve' || true
```

Then re-run `make external-auth-smoke`.

### Docker container issues

Symptom:
- Keycloak readiness timeout.

Fix:

```bash
docker --version
docker logs kc-oauth-smoke --tail 200 || true
docker rm -f kc-oauth-smoke kc-oauth-test >/dev/null 2>&1 || true
```

### Missing Nebari Python venv

Symptom:
- Script cannot find Nebari Python runtime.

Fix:

```bash
cd /workspace/nebari
python3 -m venv /workspace/.venvs/nebari
/workspace/.venvs/nebari/bin/pip install -e .
```

Override at run time:

```bash
NEBARI_VENV_PYTHON=/abs/path/to/python make external-auth-smoke
```

### Missing Go binary path

Symptom:
- Script cannot find Go at default location.

Fix:

```bash
GO_BIN=$(command -v go) make external-auth-smoke
```

## Useful Logs

```bash
cat /tmp/nebari-external-auth-smoke.log || true
cat /tmp/nebi-external-auth-smoke-18460.log || true
cat /tmp/nebi-external-auth-smoke-18461.log || true
cat /tmp/mock-broker-smoke.log || true
```
