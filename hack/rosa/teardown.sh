#!/usr/bin/env bash
# hack/rosa/teardown.sh — one-command ROSA HCP teardown.
# Usage: hack/rosa/teardown.sh [cluster-name]   (default: nebari-ocp-poc)
set -euo pipefail
CLUSTER="${1:-nebari-ocp-poc}"
echo ">> Deleting ROSA cluster: $CLUSTER (this watches until gone)"
rosa delete cluster --cluster "$CLUSTER" --yes --watch
echo ">> Cleaning up operator roles + OIDC provider"
rosa delete operator-roles --cluster "$CLUSTER" --mode auto --yes || true
rosa delete oidc-provider  --cluster "$CLUSTER" --mode auto --yes || true
echo ">> Teardown complete. Verify with: rosa list clusters"
