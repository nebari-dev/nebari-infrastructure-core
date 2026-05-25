# Longhorn UI Group-Based Authorization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restrict Longhorn UI access to members of the `longhorn-admins` Keycloak group by extending the existing SecurityPolicy with a JWT provider and an authorization rule.

**Architecture:** Single-resource change. The existing `longhorn-securitypolicy.yaml` gains three additions: `oidc.forwardAccessToken: true` (Envoy injects the access token as a Bearer header on the upstream request), a `jwt` provider block (validates that bearer token using Keycloak's external issuer and in-cluster JWKS endpoint), and an `authorization` block (Deny by default; Allow only when the JWT's `groups` claim contains `"longhorn-admins"`). No Go code changes, no new files, no realm-setup changes — the prerequisite groups scope is already attached to the longhorn Keycloak client by PR #328.

**Tech Stack:** Envoy Gateway v1.6.2 SecurityPolicy CRD (`gateway.envoyproxy.io/v1alpha1`), Go `text/template` (existing template renderer in `pkg/argocd/writer.go`).

**Spec:** `docs/superpowers/specs/2026-05-25-longhorn-ui-group-authz-design.md`

**Branch:** Continues on `tpotts/longhorn-ui-gateway` (PR #328).

---

## File Structure

**Modified files:**
- `pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml` — extend `spec` with `oidc.forwardAccessToken`, `jwt`, and `authorization` blocks.
- `pkg/argocd/writer_test.go` — extend the positive sub-test of `TestWriteAllToGit_LonghornSecurityPolicy` to assert the new fields render.

**Untouched but worth knowing about (the design relies on these):**
- `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml` — already attaches the `groups` scope to the `longhorn` client (lines 172-180) and the access-token claim mapper has `"access.token.claim":"true"` (line 118).
- `pkg/argocd/writer.go` `TemplateData` — already exposes `Domain`, `KeycloakBasePath`, `KeycloakServiceURL`. No change.

---

## Task 1: Extend the SecurityPolicy template with group-based authz

**Files:**
- Modify: `pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml`
- Modify: `pkg/argocd/writer_test.go`

- [ ] **Step 1.1: Extend the positive sub-test of `TestWriteAllToGit_LonghornSecurityPolicy` with assertions for the new fields**

Open `pkg/argocd/writer_test.go`. Locate the positive sub-test of `TestWriteAllToGit_LonghornSecurityPolicy` (around lines 771-807). The existing want-list ends with the line `\`logoutPath: "/oauth2/logout"\`,`. Replace the entire want-list literal so it includes the new assertions:

```go
		for _, want := range []string{
			"kind: SecurityPolicy",
			"apiVersion: gateway.envoyproxy.io/v1alpha1",
			"name: longhorn-oidc",
			"namespace: longhorn-system",
			"kind: HTTPRoute",
			"name: longhorn",
			`issuer: "https://keycloak.test.example.com/realms/nebari"`,
			"clientID: longhorn",
			"name: longhorn-oidc-client-secret",
			`redirectURL: "https://longhorn.test.example.com/oauth2/callback"`,
			`logoutPath: "/oauth2/logout"`,
			"forwardAccessToken: true",
			"jwt:",
			"name: keycloak",
			"/realms/nebari/protocol/openid-connect/certs",
			"authorization:",
			"defaultAction: Deny",
			"name: allow-longhorn-admins",
			"action: Allow",
			"valueType: StringArray",
			"longhorn-admins",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("longhorn-securitypolicy.yaml missing %q\ngot:\n%s", want, out)
			}
		}
```

Notes on what each new assertion pins:

- `forwardAccessToken: true` — proves `spec.oidc.forwardAccessToken` is rendered.
- `jwt:` — proves the new `spec.jwt` block exists (with the trailing colon to avoid false matches on the substring `jwt` appearing in URLs or other fields).
- `name: keycloak` — the JWT provider name; chosen to match the `principal.jwt.provider` reference below.
- `/realms/nebari/protocol/openid-connect/certs` — the JWKS path. Anchored at the realm/protocol path so it would fail if someone wrote a different path (e.g., `/realms/master/...`). This intentionally does NOT pin the in-cluster host (`keycloak-keycloakx-http.keycloak.svc.cluster.local`) because that string is provider-dependent template output — the path is the load-bearing part.
- `authorization:` — proves the new `spec.authorization` block exists.
- `defaultAction: Deny` — the deny-by-default behavior.
- `name: allow-longhorn-admins` — the single allow rule's name.
- `action: Allow` — the rule action.
- `valueType: StringArray` — proves the claim is matched as an array (Keycloak emits `groups` as an array).
- `longhorn-admins` — the actual group name being allowed.

Leave the existing assertions untouched; the new lines are additive. **Do not** add an assertion for `name: groups` — that substring already matches `name: longhorn-frontend-groups`-style accidental hits and risks brittleness. The combination of `valueType: StringArray` + `longhorn-admins` is sufficient to pin the claim rule.

- [ ] **Step 1.2: Run the test, confirm it fails**

```
go test -run TestWriteAllToGit_LonghornSecurityPolicy ./pkg/argocd/ -v
```

Expected: FAIL — the first new assertion to miss is `forwardAccessToken: true`. The error output will list every missing substring. The negative sub-test (`renders empty when LonghornEnabled is false`) should still PASS unchanged.

- [ ] **Step 1.3: Edit the SecurityPolicy template**

Open `pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml`. The current body (lines 1-28 inclusive) is:

```yaml
{{- if .LonghornEnabled }}
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: longhorn-oidc
  namespace: longhorn-system
  labels:
    app.kubernetes.io/name: longhorn
    app.kubernetes.io/managed-by: nebari-infrastructure-core
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: longhorn
  oidc:
    provider:
      issuer: "https://keycloak.{{ .Domain }}{{ .KeycloakBasePath }}/realms/nebari"
    clientID: longhorn
    clientSecret:
      name: longhorn-oidc-client-secret
    redirectURL: "https://longhorn.{{ .Domain }}/oauth2/callback"
    logoutPath: "/oauth2/logout"
    scopes:
      - openid
      - profile
      - email
      - groups
{{- end }}
```

Replace the entire file with:

```yaml
{{- if .LonghornEnabled }}
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: longhorn-oidc
  namespace: longhorn-system
  labels:
    app.kubernetes.io/name: longhorn
    app.kubernetes.io/managed-by: nebari-infrastructure-core
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: longhorn
  oidc:
    provider:
      issuer: "https://keycloak.{{ .Domain }}{{ .KeycloakBasePath }}/realms/nebari"
    clientID: longhorn
    clientSecret:
      name: longhorn-oidc-client-secret
    redirectURL: "https://longhorn.{{ .Domain }}/oauth2/callback"
    logoutPath: "/oauth2/logout"
    scopes:
      - openid
      - profile
      - email
      - groups
    forwardAccessToken: true
  jwt:
    providers:
      - name: keycloak
        issuer: "https://keycloak.{{ .Domain }}{{ .KeycloakBasePath }}/realms/nebari"
        remoteJWKS:
          uri: "{{ .KeycloakServiceURL }}/realms/nebari/protocol/openid-connect/certs"
  authorization:
    defaultAction: Deny
    rules:
      - name: allow-longhorn-admins
        action: Allow
        principal:
          jwt:
            provider: keycloak
            claims:
              - name: groups
                valueType: StringArray
                values:
                  - longhorn-admins
{{- end }}
```

Three additions, all under `spec`:
- `oidc.forwardAccessToken: true` appended after `scopes:` (still inside `spec.oidc`).
- `jwt.providers[]` block at the same indent level as `oidc:` (sibling of `oidc`, not nested).
- `authorization` block at the same indent level (sibling of `oidc` and `jwt`).

`{{ .Domain }}`, `{{ .KeycloakBasePath }}`, and `{{ .KeycloakServiceURL }}` are already on `TemplateData` (see `pkg/argocd/writer.go` `NewTemplateData`). The test substitutes `Domain: "test.example.com"` and zero-value `KeycloakBasePath`, and `KeycloakServiceURL` is computed inside `NewTemplateData` as `http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080<KeycloakBasePath>` — so with the empty default base path, the rendered JWKS URI is exactly `http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080/realms/nebari/protocol/openid-connect/certs`. The substring assertion in Step 1.1 (`/realms/nebari/protocol/openid-connect/certs`) matches the tail of that string.

- [ ] **Step 1.4: Run the test, confirm it passes**

```
go test -run TestWriteAllToGit_LonghornSecurityPolicy ./pkg/argocd/ -v
```

Expected: PASS for both sub-tests:
- `includes SecurityPolicy when LonghornEnabled is true` — PASS (all 21 substrings present).
- `renders empty when LonghornEnabled is false` — PASS (file body is still entirely inside the existing `{{- if .LonghornEnabled }}` conditional).

- [ ] **Step 1.5: Run the full argocd package suite to catch any regressions**

```
go test ./pkg/argocd/...
```

Expected: all PASS. In particular `TestServiceHTTPRoutes_TargetHTTPSListener` (which walks the `routes/` directory but NOT the `policies/` directory) should be unaffected.

- [ ] **Step 1.6: Run lint**

```
golangci-lint run ./pkg/argocd/...
```

Expected: 0 issues. (No Go changes; this is a safety check that the test-file edit didn't introduce style issues.)

- [ ] **Step 1.7: Commit**

```bash
git add pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml pkg/argocd/writer_test.go
git commit -m "feat(argocd): restrict Longhorn UI to longhorn-admins via JWT-claim authz"
```

---

## Task 2: Update PR #328 description to drop the group-authz follow-up note

This task is operator-driven, not code. It runs after Task 1's commit is pushed.

**Files:** None (modifies PR description on GitHub).

- [ ] **Step 2.1: Push the new commit**

```
git push
```

Expected: the new commit lands on `origin/tpotts/longhorn-ui-gateway`.

- [ ] **Step 2.2: Update the PR description**

The current PR description ends with:

```markdown
## Follow-ups (out of scope)

- Group-based authorization on the `SecurityPolicy` (currently auth-only; groups created but not enforced)
- Client-secret rotation on redeploy (same pre-existing divergence risk as today's ArgoCD client)
```

Replace it with:

```markdown
## Follow-ups (out of scope)

- Read-only access for `longhorn-viewers` (Longhorn UI has no read-only mode; group remains a no-op for now)
- Client-secret rotation on redeploy (same pre-existing divergence risk as today's ArgoCD client)
```

Also update the summary section to reflect the stricter access policy. Find the line that currently reads:

> Authorization is **authentication-only** in this cut — any logged-in Keycloak user reaches the UI. Group enforcement is tracked as a follow-up (groups are created so a future change can wire `SecurityPolicy` group claim gating without touching Keycloak).

Replace with:

> Authorization gates the UI to members of the **`longhorn-admins` Keycloak group**. Non-members get HTTP 403 from the gateway. The `longhorn-viewers` group is created but currently a no-op (Longhorn has no read-only mode).

Update the PR via:

```bash
gh pr edit 328 --body "$(cat <<'EOF'
<the full revised PR body>
EOF
)"
```

Or paste the revised body via the GitHub UI.

- [ ] **Step 2.3: Append a new bullet to the Test Plan section**

The current Test Plan operator-driven checklist ends with `Set aws.longhorn.enabled: false, redeploy → ...`. Add four new checklist items below it (the group-authz acceptance steps from the spec):

```markdown
- [ ] Sign in as the realm admin user (already in `longhorn-admins`) → land on the Longhorn UI
- [ ] Create a fresh Keycloak user, do NOT add to `longhorn-admins`, sign in → expect HTTP 403 from the gateway
- [ ] Add that user to `longhorn-admins` via the Keycloak UI, sign out and back in → land on the Longhorn UI
- [ ] Confirm browser network tab shows OIDC redirect → callback → final request includes `Authorization: Bearer <jwt>` from forwardAccessToken (optional — only verifiable via Envoy debug)
```

---

## Self-Review

Walked the spec against the plan:

- **Spec section "Why this works on the realm side"** → no task needed (existing realm-setup is already correct; spec confirms no changes required).
- **Spec section "Architecture"** → Task 1 implements the three SecurityPolicy additions exactly as described.
- **Spec section "Template Change"** → Task 1 Step 1.3 contains the full replacement template body, byte-for-byte matching the spec snippet.
- **Spec section "Failure Modes"** → no task needed (failure modes are runtime properties of EG, not implementation choices).
- **Spec section "Filter-Ordering Caveat"** → no task needed (worth flagging during manual acceptance; if it triggers, fall back to oauth2-proxy per the spec).
- **Spec section "Testing"** → Task 1 Step 1.1 extends the existing test's positive assertion list with all the substrings the spec calls for, plus the additional anchoring assertions (`valueType: StringArray`, `defaultAction: Deny`, `action: Allow`) to pin claim-rule structure.
- **Spec section "Manual Acceptance"** → Task 2 adds these to the PR description.
- **Spec section "Out of Scope"** → Task 2 updates the PR follow-ups section.

No gaps. No placeholders. The plan is one substantive code change (Task 1) plus a PR-description housekeeping pass (Task 2).
