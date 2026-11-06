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
- `scripts/gen-fixtures.sh`: filmovi (H.264, 4K HEVC 10-bit, HDR10, AV1, multi-audio + poglavlja + embedded/external SRT/ASS, verzije, trailer), serija sa zajedničkim introm (2×4 ep.), multi-episode, specials, daily, anime absolute, glazba (FLAC/MP3), fotke.

## Sljedeći koraci
1. Push brancha i PR → CI zelen (blokirano: vidi dolje).
2. Provjeriti iOS build u CI-ju (`ios.yml`) i instalaciju .ipa kroz Feather → spajanje na demo.streamero.net.
3. Deploy dev stacka na demo.streamero.net (Caddy ispred `:8787`).
4. Zamijeniti placeholder logo službenim s streamero.net.
5. Nakon prihvaćanja P0 → P1 (korisnici i auth).

## Poznati problemi / blokeri
- **Push na GitHub odbijen** iz cloud sessiona: Claude GitHub App nije instaliran za org `streamero-app`. Commitovi su lokalni na `phase/P0-foundations`.
- **streamero.net / demo.streamero.net nedostupni** iz sandboxa (mrežna politika) → logo nije preuzet, deploy na server nije napravljen.
- iOS projekt nije kompajliran (nema macOS-a u sandboxu) — prva provjera će biti `ios.yml` u CI-ju.
- Runtime stage Dockerfilea (apt: Debian + repo.jellyfin.org) nije buildan u sandboxu (mreža); web i Go stageovi jesu. Compose stack testiran sa sandbox runtime imageom bez ffmpega.
- `scripts/seed-big-library.sh` dolazi u P2 (treba scanner/shemu).
