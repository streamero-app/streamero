# PROGRESS

## Trenutna faza
P0 — Temelji (u tijeku, branch `phase/P0-foundations`)

## Gotovo
- Monorepo struktura po PLAN.md §2, `.gitignore`, `.claude/settings.json`.
- `api/openapi.yaml` (system/info, system/ping) + `scripts/gen-api.sh` (ogen → Go, openapi-typescript → web, kopija spec za swift-openapi-generator).
- Go server: `serve | worker | migrate | healthcheck | version`; `/healthz`, `/readyz`, `GET /api/v1/system/info`; 503 + `Retry-After` dok se pokreće; goose + River migracije (embedane); Valkey; stabilan server ID (UUIDv7) u `settings`; `CACHEDIR.TAG` u cache/transcode.
- Testovi: unit (router, 503, SPA fallback, cachedir) + integracijski protiv stvarnog Postgresa/Valkeya (bootstrap, migracije, start < 5 s). `golangci-lint`: 0 issues.
- Web: React 19 + Vite + TS + Tailwind v4 + TanStack Router/Query, i18n HR/EN, dizajn tokeni na CSS varijablama, login ekran (ljuska), ServerGate (čeka 503 → ready), embed u Go binarij. Vitest (4) + Playwright smoke (3) prolaze.
- Deploy: multi-stage `Dockerfile` (jellyfin-ffmpeg8 ili Debian ffmpeg), `docker-compose.yml` + `compose.dev.yml` + GPU overridei, `.env.example`. Stack podignut kroz compose: db/valkey/streamero healthy, worker obrađuje jobove, web i API odgovaraju.
- iOS: XcodeGen `project.yml`, SwiftUI app s ekranom „Dodaj server" (provjera kroz generirani `getSystemInfo`), HR/EN `Localizable.xcstrings`, dizajn tokeni, unit testovi za normalizaciju URL-a.
- CI: `ci.yml` (server lint/test s Postgresom, provjera svježine generiranog koda, web typecheck/test/build, e2e protiv compose stacka), `server-image.yml` (amd64+arm64 → GHCR, cosign keyless), `ios.yml` (testovi + unsigned .ipa), `release.yml` (+ `feather/source.json` update).
- Dev stack podignut na serveru (103.141.79.116): compose projekt `streamero`, `127.0.0.1:8797` (8787 zauzima drugi servis), runtime image s jellyfin-ffmpeg 8.1.3 prvi put buildan; fixtures (30 datoteka, 53 MB) generirani kroz `streamero:dev` image i mountani na `/media`. `/healthz`, `/readyz`, `/api/v1/system/info`, SPA i asseti → 200; worker heartbeat svake minute.
- Popravak: Docker kreira bind-mount direktorije kao root → server nije mogao pisati u `/cache` i `/transcode`. Dodan one-shot `init` servis (chown na uid 1000) i `mode: 01777` za transcode tmpfs.
- Reverse proxy: nginx umjesto Caddyja (D-010), site u `deploy/nginx/demo.streamero.net.conf` (sintaksa provjerena `nginx -t`).
- Uklonjen stari, nepovezani compose projekt `streamero` (`/opt/streamero`: api/worker/nginx/postgres/redis/s3 + volumeni) na zahtjev korisnika.
- demo.streamero.net živ preko nginxa + Let's Encrypt (HTTP→HTTPS, `/readyz` 200).
- Branch pushan, PR #1 (`phase/P0-foundations` → `main`) otvoren; `main` je prazan početni commit spojen u P0 (repo je bio prazan). CI zelen: `server`, `web`, `e2e`, iOS `build` (prvi uspješan iOS build, artefakt `streamero-ipa`).
- `scripts/gen-fixtures.sh`: filmovi (H.264, 4K HEVC 10-bit, HDR10, AV1, multi-audio + poglavlja + embedded/external SRT/ASS, verzije, trailer), serija sa zajedničkim introm (2×4 ep.), multi-episode, specials, daily, anime absolute, glazba (FLAC/MP3), fotke.

## Sljedeći koraci
1. Review i merge PR #1 (prihvaćanje P0) → `server-image.yml` builda i potpisuje multi-arch image (`edge`) na GHCR-u.
2. Instalirati `streamero-ipa` kroz Feather i spojiti se na https://demo.streamero.net.
3. Zamijeniti placeholder logo službenim s streamero.net.
4. Nakon prihvaćanja P0 → P1 (korisnici i auth).

## Poznati problemi / blokeri
- Vizualna provjera weba u pravom browseru nije napravljena na serveru (nema Chromiuma/Playwrighta na hostu); provjereno preko HTTP-a (SPA + asseti 200, HR/EN stringovi u bundleu).
- `server-image.yml` ručno pokrenut s feature brancha nema tagova (samo `main`/`v*`) i pao bi; multi-arch (arm64) image još nije buildan.
- Claude na serveru nema sudo: nginx/certbot/apt promjene radi korisnik.
- `scripts/seed-big-library.sh` dolazi u P2 (treba scanner/shemu).
