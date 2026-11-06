# Streamero

Self-hosted media server with a web client and an iOS app. Brand: SCYDE.

> Status: early development (phase P0, foundations). See `docs/PROGRESS.md`.

## Quick start (Docker Compose)

```bash
cd deploy
cp .env.example .env        # set DB_PASSWORD and MEDIA_PATH
docker compose up -d        # add -f compose.vaapi.yml / compose.nvidia.yml for GPU
```

Open `http://<host>:8787`. Put a reverse proxy (e.g. nginx, see `deploy/nginx/`) in front for TLS and remote access.

## Development

| What | Command |
|---|---|
| Dev stack (builds locally) | `docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml up -d --build` |
| Server tests | `cd server && go test ./... -race && golangci-lint run` |
| API codegen | `./scripts/gen-api.sh` |
| Web | `cd web && pnpm i && pnpm dev` |
| Test media | `./scripts/gen-fixtures.sh ./data/fixtures` |
| iOS | `cd ios && xcodegen generate` (macOS) |

`api/openapi.yaml` is the source of truth for the API; never edit generated code by hand.

Docs: [`docs/PLAN.md`](docs/PLAN.md) · [`docs/FEATURES.md`](docs/FEATURES.md) · [`docs/DECISIONS.md`](docs/DECISIONS.md) · [`docs/DEPLOY.md`](docs/DEPLOY.md)

## License

All rights reserved (private, pre-release). Third-party components: [`THIRD_PARTY.md`](THIRD_PARTY.md).
