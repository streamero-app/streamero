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
Data (database, config, cache) lives in `./data` next to the compose file — see
[Backup and restore](#backup-and-restore).

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


## Backup and restore
Streamero backs itself up (D-030): **Settings → Backups**, admins only. A backup is one
`.tar.gz` holding a consistent snapshot of the whole database (users, libraries, watch history,
settings) and the `/config` folder; downloaded artwork only when "Include artwork" is on. Media
files are never part of a backup.

- Backups are written to `STREAMERO_BACKUP_DIR` (default `/config/backups`, i.e.
  `./data/config/backups` on the host) as `streamero-<UTC time>-<version>.tar.gz`. Copy that
  folder off the server (rsync, restic, …) — a backup on the same disk does not survive the disk.
- A daily backup runs by default; the last 7 scheduled/manual backups are kept (configurable).
  "Back up now" queues one immediately. A backup taken during a library scan is still consistent
  but may miss titles being added at that moment; the UI warns.
- The same page downloads, deletes, uploads (from another server) and restores backups. A
  restore asks you to type the backup name, writes a `streamero-pre-restore-…` backup of the
  current state first, replaces the database and config, and restarts the server.
- A backup from a **newer** Streamero version is refused (update the server first). A backup
  from an **older** version is restored into its own schema and then migrated forward.

From the command line (e.g. a scheduled extra backup): `docker compose exec streamero /app/streamero backup [--images]`.

### Restoring a broken install from the CLI
When the web UI no longer starts (damaged database, failed upgrade, new server), restore with
the CLI. It needs only the database and the backup file:
```bash
cd /opt/streamero
docker compose stop streamero worker          # nothing else may write while restoring
docker compose up -d db valkey
# a backup in ./data/config/backups is found by name:
docker compose run --rm --no-deps streamero restore streamero-20261110T030000Z-0.9.0.tar.gz
# a file from elsewhere: copy it into ./data/config/backups first, or mount it:
docker compose run --rm --no-deps -v /path/to/backup.tar.gz:/restore.tar.gz:ro streamero restore /restore.tar.gz
docker compose up -d
```
The command prints what the backup contains and asks you to type `RESTORE` (`--yes` skips the
question in scripts). Options: `--no-safety-backup` (do not back up the current state first,
e.g. when the database is empty or unreadable) and `--skip-files` (database only, leave
`/config` and avatars as they are). On a fresh server, start from an empty `./data` with the
same `.env` (`DB_PASSWORD`), run `docker compose up -d db valkey`, then restore as above.

## Health
- `GET /healthz` — process is alive
- `GET /readyz` — database, cache and migrations ready (503 + `Retry-After` while starting)
