# Deploying Streamero

## Requirements
- Docker Engine with the Compose plugin
- ~2 GB RAM free (more for 4K transcoding); optional GPU (Intel/AMD VA-API or NVIDIA)

## Servers
- **Development** (demo.streamero.net): builds from source (`compose.dev.yml`), test media only.
- **Stable / production**: a separate server running the published image from GHCR with
  `docker-compose.yml` alone — no source checkout, no build.

## Install (stable server)
Only two files are needed:
```bash
mkdir -p /opt/streamero && cd /opt/streamero
curl -fsSLO https://raw.githubusercontent.com/streamero-app/streamero/main/deploy/docker-compose.yml
curl -fsSL  https://raw.githubusercontent.com/streamero-app/streamero/main/deploy/.env.example -o .env
```
Edit `.env`:
| Key | Value |
|---|---|
| `DB_PASSWORD` | a long random string (`openssl rand -hex 24`) |
| `MEDIA_PATH` | your media folder on this server, e.g. `/srv/media` (mounted read-only at `/media`) |
| `STREAMERO_VERSION` | a release such as `0.2.0` (or `edge` = latest `main`); `latest` = newest release |
| `HTTP_PORT` | `8787`, or `127.0.0.1:8787` behind a reverse proxy |
| `STREAMERO_TRUSTED_PROXIES` | only behind a reverse proxy (see below) |
| `TZ` | e.g. `Europe/Zagreb` |

Start and create the admin:
```bash
docker compose up -d
docker compose logs streamero | grep setup_code   # one-time setup code
```
Open `http://<server>:8787`, enter the setup code and create the admin account.
Data (database, config, cache) lives in `./data` next to the compose file — back it up.

Update: change `STREAMERO_VERSION` if pinned, then `docker compose pull && docker compose up -d`
(migrations run automatically).

GPU:
```bash
docker compose -f docker-compose.yml -f compose.vaapi.yml up -d   # Intel/AMD (set RENDER_GID)
docker compose -f docker-compose.yml -f compose.nvidia.yml up -d  # NVIDIA (nvidia-container-toolkit)
```

## Media folders
`MEDIA_PATH` (in `deploy/.env`) is mounted read-only at `/media`; libraries can only use folders
below `STREAMERO_MEDIA_ROOTS` (default `/media`). Streamero never writes to or deletes from them.
Suggested layout:
```
Movies/Dune (2021)/Dune (2021).mkv            # versions: "Dune (2021) - 2160p.mkv"
Movies/Dune (2021)/Dune (2021).hr.srt         # sidecar subtitles (lang, .forced)
Shows/Severance (2022)/Season 01/Severance S01E01.mkv
```
Copy media to `MEDIA_PATH` (e.g. `rsync -av Movies/ server:/srv/media/movies/`), then
Settings → Libraries → Add library → `/media/movies`.

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

### Client IPs behind the proxy
Set `STREAMERO_TRUSTED_PROXIES` in `deploy/.env` to the address the proxy connects from as the
container sees it (proxy on the Docker host → the compose network gateway, e.g. `172.18.0.1`).
Only then is `X-Forwarded-For` used; sign-in endpoints are rate limited per client IP
(20/min, burst 10) and the audit log records it.

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


## Health
- `GET /healthz` — process is alive
- `GET /readyz` — database, cache and migrations ready (503 + `Retry-After` while starting)
