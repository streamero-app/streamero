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

## D-011 *arr ekosustav i Seerr/Jellyseerr kao obavezni, ne „extras" (2026-11-06)
**Kontekst:** Korisnik traži da Streamero radi s Prowlarr, Radarr, Sonarr, Lidarr, Bazarr, Seerr/Jellyseerr i ostatkom *arr stacka. Plan ih je imao u P13 (requests, webhooks) i P14 (opcionalni Jellyfin-compat sloj). Ti alati se na media server spajaju na tri načina:
- **Seerr / Jellyseerr / Overseerr-fork** podržavaju samo Plex, Jellyfin i Emby kao media server (login korisnika, uvoz korisnika, sync dostupnosti po TMDB/TVDB ID-u, „Play on" linkovi). Bez Jellyfin-kompatibilnog API-ja ne mogu raditi.
- **Radarr / Sonarr / Lidarr** (i Whisparr, Readarr-forkovi) u *Connect* imaju „Emby / Jellyfin" (poziv da je putanja promijenjena → refresh) i generički *Webhook* (JSON s putanjama).
- **Bazarr** piše `.srt`/`.ass` pokraj datoteke i ne zove media server; **Prowlarr** sinkronizira indeksere samo prema *arr aplikacijama i nema nikakvu vezu s media serverom.
**Odluka:** Nova faza **P4b — *arr ekosustav** (nakon P4, jer treba korisnike iz P1, scanner iz P2, TMDB/TVDB ID-eve iz P3 i stranice detalja iz P4 za „Play on" linkove):
1. **Compat API podskup** (prije P14 „opcionalno") pod prefiksom `/compat` (konfigurabilno), implementiran isključivo po javnoj OpenAPI specifikaciji i javnoj dokumentaciji klijenata; samo endpointi koje Seerr/Jellyseerr stvarno koriste (auth po imenu, javni system info, korisnici, biblioteke/views, items s ProviderIds, sezone/epizode, nedavno dodano, library refresh). Svugdje u odgovorima ProductName ostaje „Streamero" — nigdje „Jellyfin" u UI-ju ni identifikatorima.
2. **Ulazni webhooks za *arr**: generički *Webhook* payload Radarra/Sonarra/Lidarra i compat „Emby/Jellyfin" notifikacija → ciljani scan samo zahvaćene putanje (kroz `library.ResolveSafePath`).
3. **Sidecar podnaslovi (Bazarr)**: fsnotify + periodični lagani scan sidecar datoteka; dodatno API endpoint za Bazarrov post-processing hook.
4. **Prowlarr**: nema integracije s media serverom — ništa se ne implementira osim (opcionalno, P10) prikaza zdravlja *arr servisa na admin dashboardu.
5. Vlastiti Requests (P13) ostaje, ali Seerr/Jellyseerr mora raditi i bez njega.
**Posljedice:** Compat sloj se sad održava kao javni ugovor; testira se e2e protiv **stvarnih kontejnera** Seerr, Jellyseerr, Radarr, Sonarr, Lidarr i Bazarr u CI-ju (compose profil `arr`). Podržani podskup je dokumentiran u `docs/INTEGRATIONS.md`; nepodržani endpointi vraćaju 404, ne lažne podatke. P14 „Jellyfin-compat za Infuse/Swiftfin" se nadograđuje na isti sloj.

## D-012 Sesije: Bearer token za aplikacije, HttpOnly cookie za web; setup kod (2026-11-06)
**Kontekst:** PLAN §5.5 predviđa header `Authorization: Streamero token="…"`. Web klijent ne bi smio držati token u JavaScriptu (XSS), a demo je javno dostupan pa bi prvi posjetitelj mogao „preuzeti" server kroz setup wizard.
**Odluka:**
- Standardni `Authorization: Bearer <token>` (OpenAPI `http/bearer`, ogen ga generira) za iOS i integracije; web dobiva isti token kao `streamero_session` cookie (`HttpOnly`, `SameSite=Strict`, `Secure` kad je veza HTTPS, i iza proxyja preko `X-Forwarded-Proto`). Server cookie interno pretvara u Bearer.
- Token: 32 B random, u bazi samo SHA-256; sesija vrijedi 30 dana i produžuje se korištenjem.
- Prvi admin se može napraviti samo uz jednokratni **setup kod** koji server ispisuje u log pri startu (`setup_code=XXXX-XXXX`) ili je zadan s `STREAMERO_SETUP_CODE`; 10 pogrešnih kodova → 1 min pauze.
- Lozinke min. 8 znakova (argon2id); lockout 5 pogrešnih → 15 min, atomično u SQL-u; nepostojeći korisnik odgovara isto kao kriva lozinka.
- Rate limit po IP-u odgođen do postavke „known proxies" (D-009) — iza nginxa bi svi klijenti imali isti IP.
**Posljedice:** API ključevi (P1, sljedeći korak) koriste isti Bearer header. CSRF: mutacije su JSON (cross-site forma ih ne može poslati bez CORS preflighta), a cookie je `SameSite=Strict`.

## D-013 Dozvole kao jsonb s defaultima u kodu; limit uređaja odjavljuje najstariji (2026-11-06)
**Kontekst:** P1 traži model dozvola prije nego što postoje biblioteke, reprodukcija i Live TV; Jellyfin pri prekoračenju broja sesija odbija novu prijavu.
**Odluka:** `users.permissions jsonb`; nedostajući ključevi dobivaju `auth.DefaultPermissions()` pri čitanju, pa nove dozvole ne trebaju migraciju podataka. Admin uvijek smije sve. Kad korisnik s limitom N uđe na N+1. uređaj, odjavljuje se najdulje neaktivna sesija (umjesto odbijanja), jer bi inače korisnik sa zaboravljenom sesijom ostao zaključan.
**Posljedice:** Svaka buduća značajka mora provjeriti svoju dozvolu (`Principal.User.Permissions`) i imati test za to.
