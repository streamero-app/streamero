#!/usr/bin/env bash
# Generates API code from api/openapi.yaml for server (ogen), web (openapi-typescript).
# The iOS app is no longer built (D-055), so its spec copy is no longer synced.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPEC="$ROOT/api/openapi.yaml"

echo "==> server (ogen)"
(cd "$ROOT/server" && go tool ogen --target internal/api/gen --package gen --clean "$SPEC")

echo "==> web (openapi-typescript)"
(cd "$ROOT/web" && pnpm exec openapi-typescript "$SPEC" -o src/api/schema.gen.ts)

echo "done"
