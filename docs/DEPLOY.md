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

## Reverse proxy (nginx)
`deploy/nginx/demo.streamero.net.conf` is a ready site file: buffering off (streaming and range
requests), 1 h timeouts and WebSocket upgrade. Bind Streamero to localhost only
(`HTTP_PORT=127.0.0.1:<port>` in `deploy/.env`) and point `proxy_pass` at that port.

```bash
sudo cp deploy/nginx/demo.streamero.net.conf /etc/nginx/sites-available/demo.streamero.net
sudo ln -s /etc/nginx/sites-available/demo.streamero.net /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d demo.streamero.net   # adds 443 + HTTP->HTTPS redirect
```

## Development instance (demo.streamero.net)
The dev instance builds from source on the server. Port 8787 is taken by another service on that
host, so `deploy/.env` sets `HTTP_PORT=127.0.0.1:8797` and nginx proxies to it.
```bash
git pull
# test media (ffmpeg on the host, or via the built image):
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/w" -w /w --entrypoint bash streamero:dev \
  scripts/gen-fixtures.sh ./data/fixtures
./scripts/deploy-demo.sh   # = compose up --build with version/commit, then checks the public URL
```
Every change is deployed this way; `GET /api/v1/system/info` on the demo shows the branch
(`version: dev-<branch>`) and commit it was built from (`-dirty` = uncommitted changes).

## Update (production)
```bash
docker compose pull && docker compose up -d
```

## Health
- `GET /healthz` — process is alive
- `GET /readyz` — database, cache and migrations ready (503 + `Retry-After` while starting)
