#!/usr/bin/env bash
# Generates API code from api/openapi.yaml for server (ogen), web (openapi-typescript).
# The iOS client is generated at build time by swift-openapi-generator (Xcode build plugin),
# which reads ios/Streamero/openapi.yaml — a copy kept in sync by this script.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPEC="$ROOT/api/openapi.yaml"

echo "==> server (ogen)"
(cd "$ROOT/server" && go tool ogen --target internal/api/gen --package gen --clean "$SPEC")

echo "==> web (openapi-typescript)"
(cd "$ROOT/web" && pnpm exec openapi-typescript "$SPEC" -o src/api/schema.gen.ts)

echo "==> ios (spec copy for swift-openapi-generator)"
cp "$SPEC" "$ROOT/ios/Streamero/openapi.yaml"

echo "done"
