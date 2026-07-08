#!/usr/bin/env bash
# Verify enterprise CA-bundle propagation against a mock TLS-inspecting proxy.
#
# Implements the verification recipe for issue #312 (child of the enterprise
# CA-bundle epic #307). It stands up mitmproxy as a TLS-inspecting egress proxy
# that re-signs every upstream certificate with a throwaway "org CA", then
# drives an outbound HTTPS clone from the ArgoCD repo-server through it and
# asserts that:
#
#   1. WITH the propagated bundle (SSL_CERT_FILE/GIT_SSL_CAINFO set by the
#      trust_bundle feature) the clone SUCCEEDS, and
#   2. WITHOUT it (system CAs only) the clone FAILS with an unknown-authority
#      error -- proving the success in (1) is due to the injected org CA and
#      not something else.
#
# Prerequisite: a running cluster (kind or k3d) whose node(s) sit on a docker
# network, already deployed with `trust_bundle` pointing at the CA certificate
# this script uses (see --ca-cert / --gen-ca and docs/verify-tls-proxy.md).
#
# This harness covers the repo-server (the clearest DoD, per #310/#353). Other
# components (Keycloak outbound, user pods) can be exercised with the same
# proxy by pointing their client at http://<proxy-ip>:8080; see the docs.
set -euo pipefail

KUBECONFIG_PATH="${KUBECONFIG:-}"
DOCKER_NETWORK=""
CA_CERT=""
CA_KEY=""
GEN_CA=""
TARGET_URL="https://github.com/argoproj/argo-cd"
PROXY_NAME="nic-tls-proxy-verify"
PROXY_PORT="8080"

usage() {
  cat <<EOF
Usage: $0 --kubeconfig PATH --docker-network NAME [--ca-cert FILE --ca-key FILE | --gen-ca DIR] [--target-url URL]

  --kubeconfig       kubeconfig for the target cluster (or set KUBECONFIG)
  --docker-network   docker network the cluster node(s) are attached to
                     (kind: 'kind'; k3d: 'k3d-<clustername>')
  --ca-cert/--ca-key PEM cert + key of the org CA the cluster was deployed to
                     trust (trust_bundle). The proxy signs with this.
  --gen-ca DIR       instead of --ca-cert/--ca-key, generate a throwaway org CA
                     into DIR (org-ca.crt/org-ca.key) and print the cert path
                     to deploy with, then exit (run again with --ca-cert/key).
  --target-url       HTTPS git URL to clone through the proxy (default: $TARGET_URL)
EOF
  exit "${1:-0}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --kubeconfig) KUBECONFIG_PATH="$2"; shift 2;;
    --docker-network) DOCKER_NETWORK="$2"; shift 2;;
    --ca-cert) CA_CERT="$2"; shift 2;;
    --ca-key) CA_KEY="$2"; shift 2;;
    --gen-ca) GEN_CA="$2"; shift 2;;
    --target-url) TARGET_URL="$2"; shift 2;;
    -h|--help) usage 0;;
    *) echo "unknown arg: $1" >&2; usage 1;;
  esac
done

if [ -n "$GEN_CA" ]; then
  mkdir -p "$GEN_CA"
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$GEN_CA/org-ca.key" -out "$GEN_CA/org-ca.crt" \
    -days 3650 -subj "/CN=Mock Org CA - nic tls-proxy verify" >/dev/null 2>&1
  echo "Generated throwaway org CA:"
  echo "  cert: $GEN_CA/org-ca.crt   (set trust_bundle.path to this and deploy)"
  echo "  key : $GEN_CA/org-ca.key   (pass both back via --ca-cert/--ca-key to verify)"
  exit 0
fi

[ -n "$KUBECONFIG_PATH" ] || { echo "ERROR: --kubeconfig required" >&2; usage 1; }
[ -n "$DOCKER_NETWORK" ] || { echo "ERROR: --docker-network required" >&2; usage 1; }
[ -n "$CA_CERT" ] && [ -n "$CA_KEY" ] || { echo "ERROR: --ca-cert and --ca-key required (or use --gen-ca)" >&2; usage 1; }

KUBECTL="kubectl --kubeconfig $KUBECONFIG_PATH"

cleanup() { docker rm -f "$PROXY_NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "==> Building mitmproxy CA from the org CA and starting the proxy"
CONF="$(mktemp -d)"
cat "$CA_KEY" "$CA_CERT" > "$CONF/mitmproxy-ca.pem"
chmod -R a+rX "$CONF"
cleanup
docker run -d --name "$PROXY_NAME" --network "$DOCKER_NETWORK" \
  -v "$CONF:/home/mitmproxy/.mitmproxy" \
  mitmproxy/mitmproxy mitmdump --listen-port "$PROXY_PORT" \
  --set confdir=/home/mitmproxy/.mitmproxy >/dev/null
sleep 4
PROXY_IP="$(docker inspect "$PROXY_NAME" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')"
[ -n "$PROXY_IP" ] || { echo "ERROR: could not determine proxy IP on $DOCKER_NETWORK" >&2; exit 1; }
echo "    proxy at $PROXY_IP:$PROXY_PORT (signing with $(openssl x509 -in "$CA_CERT" -noout -subject))"

RS="$($KUBECTL -n argocd get pod -l app.kubernetes.io/name=argocd-repo-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
[ -n "$RS" ] || { echo "ERROR: no argocd repo-server pod found" >&2; exit 1; }
echo "==> repo-server: $RS"

echo "==> [1/2] clone through the proxy using the propagated bundle (expect SUCCESS)"
if $KUBECTL -n argocd exec "$RS" -c repo-server -- sh -c \
     "export https_proxy=http://$PROXY_IP:$PROXY_PORT HTTPS_PROXY=http://$PROXY_IP:$PROXY_PORT; git ls-remote $TARGET_URL HEAD" >/dev/null 2>&1; then
  echo "    PASS: clone succeeded (org CA is trusted via the propagated bundle)"
else
  echo "    FAIL: clone failed WITH the propagated bundle -- trust_bundle not wired?" >&2
  exit 1
fi

echo "==> [2/2] control: same clone forced onto the system-only bundle (expect FAILURE)"
if $KUBECTL -n argocd exec "$RS" -c repo-server -- sh -c \
     "export https_proxy=http://$PROXY_IP:$PROXY_PORT HTTPS_PROXY=http://$PROXY_IP:$PROXY_PORT GIT_SSL_CAINFO=/etc/ssl/certs/ca-certificates.crt; git ls-remote $TARGET_URL HEAD" >/dev/null 2>&1; then
  echo "    FAIL: clone unexpectedly succeeded with the system-only bundle -- control is invalid" >&2
  exit 1
else
  echo "    PASS: clone failed as expected (system bundle does not trust the org CA)"
fi

echo "==> OK: CA-bundle propagation verified end-to-end against the mock proxy"
