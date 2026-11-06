# PROGRESS

## Trenutna faza
P1 — Korisnici i auth (u tijeku, branch `phase/P1-auth`); P0 prihvaćen i mergean (PR #1)

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

## P1 — gotovo (branch `phase/P1-auth`)
- API (spec → ogen): `/setup`, `/auth/{login,logout,me,password}`, `/users`, `/sessions`, `/settings/server`; migracija 00002 (users, sessions, audit_log); D-012.
- Setup wizard sa setup kodom iz loga, login/odjava (cookie za web, Bearer za aplikacije), lockout, upravljanje korisnicima (admin, onemogući, lozinka, brisanje, zaštita zadnjeg admina), uređaji s trenutnim opozivom, promjena lozinke (odjavljuje ostale uređaje), ime servera.
- Web: setup, prijava, ljuska aplikacije, Postavke (Moj račun, Uređaji, Server, Korisnici, API ključevi), HR/EN.
- Pravi IP klijenta iza proxyja (`STREAMERO_TRUSTED_PROXIES`, D-014), rate limit po IP-u na prijavi/setupu/Quick Connectu/pozivnicama/PIN-u, IP u audit logu.
- Profili (do 6 po računu): ime, boja, PIN (5 pogrešnih → 5 min), dječji profil (bez postavki/admina, izlaz traži lozinku računa), dobna granica (model; provedba s ocjenama u P3); profil po sesiji; migracija 00008.
- Pozivnice: admin napravi poveznicu (rok, broj računa, dozvole), primatelj napravi običan račun i odmah je prijavljen; potrošnja je atomična; migracija 00007.
- Dozvole po korisniku (model + API + UI, zadane vrijednosti za nove ključeve; provedba po značajci) i max broj prijavljenih uređaja (prijava preko limita odjavljuje najdulje neaktivan uređaj); migracija 00006.
- Quick Connect: novi uređaj prikaže 6-znamenkasti kod (5 min), prijavljeni korisnik ga odobri u Postavkama → uređaj se prijavi tim računom; tajna za polling samo jednom; migracija 00005.
- 2FA (TOTP, RFC 6238 bez vanjske biblioteke; QR + otpauth link; 10 jednokratnih kodova za oporavak; zaštita od ponovne upotrebe koda; pogrešni kodovi ulaze u lockout; admin može poništiti 2FA); migracija 00004.
- API ključevi (`sk_…`, Bearer; rade s pravima admina koji ih je napravio, gase se ako izgubi admin prava), preimenovanje uređaja; migracija 00003.
- Testovi: Go unit + integracijski protiv Postgresa (setup, cookie/bearer, revoke, lockout, zadnji admin, promjena lozinke), Vitest 18, Playwright 9 (setup, korisnici, uređaji, Quick Connect, pozivnice, lockout, odjava — na svježem stacku).

## Sljedeći koraci
1. Zatvaranje P1: pregled koda, PR #4 iz drafta u review i merge. Zatim P2 (biblioteke i scanner).
2. Instalirati `streamero-ipa` kroz Feather i spojiti se na https://demo.streamero.net (iOS login dolazi u P7).
3. Zamijeniti placeholder logo službenim s streamero.net.
4. Nakon P1: P2 (biblioteke i scanner) … *arr ekosustav je nova faza P4b (D-011).

## Poznati problemi / blokeri
- `server-image.yml` ručno pokrenut s feature brancha nema tagova (samo `main`/`v*`) i pao bi; multi-arch (arm64) image još nije buildan.
- Claude na serveru nema sudo: nginx/certbot/apt promjene radi korisnik.
- `scripts/seed-big-library.sh` dolazi u P2 (treba scanner/shemu).
