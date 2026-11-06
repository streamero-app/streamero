#!/usr/bin/env bash
# Rebuilds the dev stack from the current working tree and verifies that the
# public demo (DEMO_URL) serves exactly this build. Run after every change.
# Usage: scripts/deploy-demo.sh
set -euo pipefail
cd "$(dirname "$0")/.."

DEMO_URL="${DEMO_URL:-https://demo.streamero.net}"
COMPOSE=(docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml)

commit="$(git rev-parse --short HEAD)"
[[ -n "$(git status --porcelain --untracked-files=no)" ]] && commit="${commit}-dirty"
branch="$(git rev-parse --abbrev-ref HEAD)"
export STREAMERO_BUILD_COMMIT="$commit"
export STREAMERO_BUILD_VERSION="dev-${branch//\//-}"

echo "==> building ${STREAMERO_BUILD_VERSION} @ ${commit}"
"${COMPOSE[@]}" up -d --build --wait --wait-timeout 180

echo "==> verifying ${DEMO_URL}"
for _ in $(seq 1 30); do
  info="$(curl -fsS -m 5 "${DEMO_URL}/api/v1/system/info" 2>/dev/null || true)"
  if grep -q "\"commit\":\"${commit}\"" <<<"$info"; then
    echo "OK: ${DEMO_URL} serves ${STREAMERO_BUILD_VERSION} @ ${commit}"
    exit 0
  fi
  sleep 2
done
echo "FAIL: ${DEMO_URL} does not serve commit ${commit}; got: ${info:-<no response>}" >&2
exit 1
