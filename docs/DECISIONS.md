# DECISIONS

Format: `## D-001 Naslov (datum)` → Kontekst → Odluka → Posljedice.

## D-001 Backend u Go-u, Postgres umjesto SQLite-a (2026-11-06)
**Kontekst:** Najveći prigovori na Jellyfin su performanse velikih (posebno glazbenih) biblioteka i zaključavanje SQLite baze.
**Odluka:** Go server + PostgreSQL 17 (pgvector, pg_trgm) + River job queue + Valkey.
**Posljedice:** Jedan dodatni kontejner (Postgres), ali skalabilnost, pravi indeksi, AI pretraga i bez DB lockova.

## D-002 OpenAPI kao izvor istine (2026-11-06)
**Odluka:** `api/openapi.yaml` generira Go server sučelja (ogen), TS klijent i Swift klijent.
**Posljedice:** Web i iOS nikad ne zaostaju za serverom; svaka promjena API-ja ide kroz spec.

## D-003 iOS: AVPlayer primarno, MPVKit fallback; bez APNs-a (2026-11-06)
**Kontekst:** App se sideloada kroz Feather; neki entitlementi ne prežive resign.
**Odluka:** AVPlayer + HLS za najbolji HDR/DV/PiP/AirPlay; MPVKit za direct play egzotičnih formata; notifikacije preko WebSocketa/ntfy umjesto APNs-a; discovery preko Bonjoura.

## D-004 Projektni identitet i infrastruktura (2026-11-06)
**Kontekst:** Otvorena pitanja iz PLAN.md §10.
**Odluka:** Repo `streamero-app/streamero` (GHCR `ghcr.io/streamero-app/streamero`); bundle ID `net.scyde.streamero`; reverse proxy Caddy (zamijenjeno nginxom, vidi D-010); GPU na serveru još nepoznat → prvi HW put koji testiramo je VA-API. Pristup je clean-room (ne fork Jellyfina). Dev instanca: demo.streamero.net, produkcija streamero.net.
**Posljedice:** `GH_OWNER` default u compose je `streamero-app`. Ako se GPU pokaže NVIDIA, NVENC put ide prvi u P5.

## D-005 Go 1.26 umjesto 1.25 (2026-11-06)
**Kontekst:** PLAN.md predviđa Go 1.25+. Aktualne verzije River, goose i go-redis zahtijevaju Go ≥ 1.26.
**Odluka:** `go 1.26.4` u `server/go.mod`, builder image `golang:1.26-trixie`.
**Posljedice:** Nema — plan je govorio „1.25+".

## D-006 TypeScript 5.9 i supply-chain politika za npm (2026-11-06)
**Kontekst:** TypeScript 7 (nativni kompajler) mijenja JS API, pa `openapi-typescript` puca. pnpm 12 (koji corepack povuče bez pina) odbija pakete mlađe od 24 h.
**Odluka:** TypeScript `~5.9` dok alati ne podrže 7. `packageManager: pnpm@10.28.0` u `web/package.json`; `web/.npmrc` s `minimum-release-age=2880` (2 dana) — svjesna zaštita od kompromitiranih svježih paketa.
**Posljedice:** Nove verzije paketa ulaze s ~2 dana zakašnjenja. Upgrade na TS 7 kad openapi-typescript doda podršku.

## D-007 Web build ide direktno u Go embed direktorij (2026-11-06)
**Odluka:** `vite build` piše u `server/internal/webui/dist` (gitignored, osim `.keep`). Go binarij servira SPA s fallbackom na `index.html`; ako web nije buildan, servira kratku poruku umjesto pada.
**Posljedice:** `go build` radi i bez Node toolchaina (za backend-only razvoj).

## D-008 Worker bez HTTP healthchecka u P0 (2026-11-06)
**Kontekst:** Image `HEALTHCHECK` gađa `/healthz`, a worker nema HTTP listener.
**Odluka:** U compose je healthcheck za `worker` isključen; živost se vidi kroz River job statistike (periodični `system.heartbeat` job svake minute).
**Posljedice:** U P10 (admin dashboard) dodati prikaz zadnjeg heartbeata workera; po potrebi mali health listener u workeru.

## D-009 Bez chi `RealIP` middlewarea (2026-11-06)
**Kontekst:** `middleware.RealIP` vjeruje `X-Forwarded-For`/`X-Real-IP` bez obzira na izvor (IP spoofing; staticcheck SA1019).
**Odluka:** Ne koristimo ga. Pravo rješenje je postavka „known proxies" (FEATURES §1, P10) koja vjeruje forward headerima samo od navedenih proxyja.
**Posljedice:** Do P10 logovi i rate limit vide IP reverse proxyja.

## D-010 nginx umjesto Caddyja za demo.streamero.net (2026-11-06)
**Kontekst:** D-004 je predvidio Caddy. Na serveru već radi sistemski nginx (80/443, certbot) za ostale domene, a port 8787 zauzima drugi servis.
**Odluka:** Reverse proxy je postojeći nginx sa site datotekom `deploy/nginx/demo.streamero.net.conf` (bez bufferinga, 1 h timeout, WebSocket upgrade); TLS kroz `certbot --nginx`. Demo stack sluša samo na `127.0.0.1:8797` (`HTTP_PORT` u `deploy/.env`).
**Posljedice:** Nema drugog procesa na 80/443. Instalacija sitea i certifikat traže root (sudo) na serveru. Za korisnike ostaje bilo koji reverse proxy; nginx primjer je u repou.
