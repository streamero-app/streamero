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

## D-014 Known proxies kao env postavka; rate limit u memoriji (2026-11-06)
**Kontekst:** D-009 je odgodio pravi IP klijenta do „known proxies". Bez toga rate limit iza nginxa vidi samo jedan IP.
**Odluka:** `STREAMERO_TRUSTED_PROXIES` (CIDR/IP lista) — `X-Forwarded-For` se čita samo kad je izravni peer na listi, i to zdesna (prvi netrusted hop), pa klijent ne može podmetnuti IP. Token bucket po IP-u u memoriji procesa (20/min, burst 10) na prijavi, setupu, Quick Connect startu, pozivnicama i PIN-u profila; IP ide u audit log. Postavka je env (ne u admin UI-ju) jer pogrešna vrijednost može zaključati sve ili otvoriti spoofing.
**Posljedice:** Više instanci servera bi trebalo dijeljeni limiter (Valkey) — tek kad bude potrebno. Demo: `172.18.0.1` (gateway compose mreže).

## D-015 Medijski bajtovi izvan OpenAPI-ja, iza HMAC potpisa (2026-11-07)
**Kontekst:** Direct play treba HTTP Range, HLS segmente i playlistu koje generira ffmpeg, a playeri (`<video>`, AVPlayer, Chromecast) ne šalju auth header. OpenAPI generator (ogen) nije prikladan za byte-range streamove.
**Odluka:** `POST /api/v1/items/{id}/playback` (u specifikaciji) odlučuje direct/remux/transcode i vraća kratkotrajne potpisane URL-ove; same bajtove poslužuje `/stream/*` izvan OpenAPI-ja: `/stream/file/{fileId}/…` (Range), `/stream/hls/{session}/…` (fMP4 HLS, playlista se prepisuje s potpisom), `/stream/sub/{fileId}/{n}.vtt` (WebVTT). Potpis = HMAC-SHA256(scope, exp) s ključem generiranim pri prvom pokretanju (u bazi); scope je prefiks `/stream/<vrsta>/<id>/`, vrijedi 8 h. Putanja iz baze prolazi kroz `ResolveSafePath` pri svakom zahtjevu. HLS koristi fMP4 segmente (radi i za HEVC/AV1 copy), ffmpeg sesije se gase nakon 90 s bez zahtjeva.
**Posljedice:** Klijenti nikad ne grade stream URL-ove sami. Napredak gledanja ide po profilu (`user_item_data.owner_id`); kod HLS-a koji počinje od `startMs` klijent zbraja offset.

## D-016 Compat API kao ručno pisani handleri pod `/compat` (2026-11-07)
**Kontekst:** D-011 traži da Seerr/Jellyseerr i *arr „Emby/Jellyfin" veza rade sa Streamerom. Njihov API oblik definira tuđa javna OpenAPI specifikacija, ne naša.
**Odluka:** `server/internal/compat` implementira samo potreban podskup (system info, AuthenticateByName, korisnici, views, items s ProviderIds i MediaSources, sezone/epizode, latest, slike, `/Auth/Keys`, `/Library/Media/Updated`, `/Library/Refresh`, `web/#/details` preusmjerenje) kao chi handlere, iz javne dokumentacije i promatranja ponašanja stvarnih klijenata (Jellyseerr 2.7.3, Radarr 5.26) — bez čitanja njihovog izvornog koda. Ne ulazi u `api/openapi.yaml` (to je naš API). `ProductName` je uvijek „Streamero". Ključ napravljen preko `/Auth/Keys` je čitljiv samo 10 min u memoriji (Streamero inače čuva samo hash). Prijava preko compata ide kroz isti rate limit.
**Posljedice:** Ponašanje je pokriveno integracijskim testom (`TestCompatAPIIntegration`); ručna provjera sa stvarnim Jellyseerrom i Radarrom opisana je u INTEGRATIONS.md. Proširenje za Infuse/Swiftfin (P14) nadograđuje isti paket.

## D-017 Vizualni identitet „projection room" (2026-11-07)
**Kontekst:** korisnik je ocijenio dotadašnji neutralni UI kao siromašan; `docs/DESIGN.md` još ne postoji.
**Odluka:** topla crna podloga i jedan jantarni akcent (projektorsko svjetlo) za primarnu akciju, fokus i napredak; serif za naslove (Instrument Serif na webu, New York na iOS-u), Geist/SF za sučelje; znak „razdvojeni play". Tokeni ostaju jedini izvor boja (`web/src/styles.css`, `ios/Streamero/DesignSystem/Theme.swift`). Smjernice iz skillova impeccable / emil-design-eng / taste-skill / awwwards-animations primijenjene bez dodavanja njihovih biblioteka.
**Posljedice:** kad korisnik doda `docs/DESIGN.md`, mijenjaju se samo tokeni i znak. Bez TMDb ključa UI koristi tipografske placeholdere umjesto praznih sivih ploha.

## D-018 Vizualni smjer „Ambijent" (2026-11-09)
**Kontekst:** korisnik je odbio Netflix raspored („ne želim kopiju Netflixa — stvori nešto naše unikatno") i od tri ponuđena smjera (Ambijent, Arena, Kinoteka) izabrao Ambijent.
**Odluka:** sučelje je tamna soba osvijetljena naslovom u fokusu. Server iz slika izvlači dvije boje (`items.colors`, migracija 00018); web ih piše u `--amb-a/--amb-b` (animirano preko `@property`), iOS u `AmbientBackground`. Početna je „pozornica" (naslov + osvijetljeni ekran s pregledom + traka postera), navigacija je plutajuća pilula, naslovi u Bricolage Grotesque. Zamjenjuje D-017 i Netflix raspored iz PR #16.
**Posljedice:** svaka nova površina koristi `--amb-*` tokene umjesto fiksne boje; primarna akcija je „lit" gumb u boji naslova; kartice svijetle vlastitom bojom (`--card-a`).

## D-019 Vizualni smjer „Kino platno" (2026-11-09)
**Kontekst:** izvedba „Ambijenta" (D-018) korisniku je izgledala djetinjasto: sve je bilo zaobljena „pilula", film je stajao u malom zaobljenom okviru, a posteri su bili nasumični kadrovi. Ponuđena su tri smjera s pravim posterima i snimkama (A „Kino platno", B „Festivalski program", C „Videoteka"); korisnik je izabrao A.
**Odluka:** film zauzima ekran: slika ide od ruba do ruba iza vrha stranice, zatamnjena prema tekstu, a svjetlo filma (`items.colors`) pada nisko lijevo iza naslova. Naslovi su postavljeni široko i velikim slovima kao u odjavnoj špici (Archivo, os širine; na iOS-u sistemski font `.width(.expanded)`). Podaci su jedan red teksta bez „pilula", kontrole su pravokutne (6 px), a primarni gumb je papirnato bijel. Boja filma pojavljuje se samo kao akcent: linija napretka, oznaka aktivne navigacije i fokus. Navigacija je tekstualna s podcrtom. Server čita `poster`/`fanart`/`logo` slike iz mape filma (pravi posteri umjesto kadrova).
**Posljedice:** zamjenjuje D-018 (tokeni `--amb-*` ostaju, ali se koriste samo za akcente). Geist i Bricolage su uklonjeni. Nove površine koriste `font-display`/`font-head`/`font-label` i pravokutne kontrole.

## D-020 Ugrađena proširenja umjesto sustava pluginova (2026-11-09)
**Kontekst:** korisnik traži „pluginove kao Jellyfin, posebno Jellyfin Enhanced, ali ugrađeno u Streamero, da se ne koristi plugin". Pluginovi treće strane unutar servera (učitavanje koda) donose sigurnosne rizike, pucaju na nadogradnjama i teško su izvedivi u Go binariju bez CGO-a; prijenos njihovog koda nije dopušten (clean-room).
**Odluka:** funkcije za koje drugi serveri koriste pluginove pišemo kao ugrađene module s prekidačima (Postavke → Proširenja, `GET/PUT /enhancements`): oznake kvalitete, „Iznenadi me", zaslon pauze, uklanjanje iz „Nastavi gledati", pomoć za prečace, slika u slici, zahtjevi u pretrazi, poveznice na Radarr/Sonarr. Integracije s vanjskim alatima idu preko njihovih javnih API-ja (Seerr, Radarr, Sonarr) i preko postojećih webhooka/obavijesti te compat API-ja.
**Posljedice:** novi „plugin" je nova funkcija u kodu s testovima i prekidačem. Ako se kasnije pokaže potreba za vanjskim proširenjima, prvi korak su webhookovi i ograničeni skriptni hookovi izvan procesa servera, ne učitavanje koda.

## D-021 iOS: obični URLSession klijent za medijski API (2026-11-09)
**Kontekst:** paritet s Androidom traži oko 100 API poziva; generirani swift-openapi klijent za svaki poziv traži po switch nad odgovorima, a lokalno nema Swift alata pa se sve provjerava tek u CI-ju.
**Odluka:** medijski i administracijski pozivi idu kroz ručno pisani `MediaClient` (URLSession + Codable modeli u `Core/API/Models.swift`) po uzoru na Android `api/Api.kt`. Prijava, Quick Connect i provjera servera ostaju na generiranom klijentu. Polja koja spec traži kao obavezna nisu opcionalna u modelima, pa se uvijek šalju (i kad su false/0); nil opcionalna polja se izostavljaju.
**Posljedice:** spec ostaje izvor istine, ali promjene API-ja treba ručno prenijeti u `Models.swift`. Titlovi se crtaju preko videa (WebVTT se dohvaća i parsira) jer AVPlayer ne može učitati vanjske titlove uz obični URL.
