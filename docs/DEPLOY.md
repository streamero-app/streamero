# Deploying Streamero

## Requirements
- Docker Engine with the Compose plugin
- ~2 GB RAM free (more for 4K transcoding); optional GPU (Intel/AMD VA-API or NVIDIA)

## Install
```bash
git clone https://github.com/streamero-app/streamero && cd streamero/deploy
cp .env.example .env
# edit .env: DB_PASSWORD, MEDIA_PATH, TZ, HTTP_PORT
docker compose up -d
```

GPU:
```bash
docker compose -f docker-compose.yml -f compose.vaapi.yml up -d   # Intel/AMD (set RENDER_GID)
docker compose -f docker-compose.yml -f compose.nvidia.yml up -d  # NVIDIA (nvidia-container-toolkit)
```

## Reverse proxy (Caddy)
```caddyfile
demo.streamero.net {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8787
}
```
WebSockets and range requests work with Caddy defaults.

## Development instance (demo.streamero.net)
The dev instance builds from source on the server:
```bash
git pull
./scripts/gen-fixtures.sh ./data/fixtures   # once
docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml up -d --build
```

## Update (production)
```bash
docker compose pull && docker compose up -d
```

## Health
- `GET /healthz` — process is alive
- `GET /readyz` — database, cache and migrations ready (503 + `Retry-After` while starting)
