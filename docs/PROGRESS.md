# PROGRESS

## Trenutna faza
P4 — Web pregledavanje (u tijeku); izdanja v0.2.0 i **v0.3.0** objavljena. P0 (PR #1) i P1 (PR #4) gotovi; prvi dio P2 (PR #5) mergean i objavljen kao **v0.2.0** (prvo izdanje za stabilni server).

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

## Popravci web playera (branch `fix/web-player-review`, 2026-11-13)
Web player: nastavak sljedeće epizode više ne dobiva poziciju prethodne; reprodukcija/pauza preživi promjenu zvuka, titla i premotavanje; odbijeni `play()` pokazuje veliki gumb za reprodukciju; druga greška pokazuje poruku s „Pokušaj ponovno"; brzina ostaje nakon restarta streama; razmak ne pokreće sljedeću epizodu; puni zaslon na iPhoneu; dodir prikazuje kontrole. Titlovi transkodiranog streama koji kreće usred filma više ne kasne (`offsetMs` na ruti titlova, D-042; Android to još treba koristiti). Live TV: oporavak od grešaka hls.js-a. Glazba staje pri odjavi/promjeni profila, shuffle ne restarta pjesmu. Promjena profila briše keš prethodnog profila. Admin akcije skrivene na dječjem profilu. Tematski dijalozi potvrde umjesto `window.confirm/prompt`. Postavke, Live TV, playeri, zajedničko gledanje, Otkrij, kolekcije i glazba učitavaju se zasebno (manji početni bundle).
## iOS popravci iz pregleda (branch `fix/ios-review`, PR #64, 2026-11-13)
Player na AVPlayerViewControlleru (PiP, auto PiP, vlastite kontrole nestaju s AVKitovim), titlovi po indeksu u datoteci + bitmap titlovi utisnuti na serveru, glazba ne preuzima lock screen tijekom videa, Live TV/preuzimanja pauziraju glazbu, odjava na 401 usred sesije, offline samo kod mrežnih grešaka, više servera (Ja → Serveri), heroji prema visini ekrana, Dynamic Type za brand fontove, straničenje glazbe, Quick Connect poll vezan uz ekran, napredak i na pauzi/seeku/pozadini, CFBundleVersion = major·10000+minor·100+patch (isti u Feather sourceu), preostali HR prijevodi.

## v0.10.1 (2026-11-11)
Reprodukcija preskače verzije koje više nisu na disku (npr. epizodu koju je Sonarr premjestio iz `downloads`); kad nijedne nema, javlja `file_missing` i ponovno skenira biblioteku (#62).

## v0.10.0 (2026-11-11)
Titlovi upisani u video i prepoznavanje uvoda po zvuku (#54), SyncPlay (#53), OIDC i LDAP prijava (#55), DLNA za televizore (#56), offline preuzimanje (#57), Live TV i snimanje (#59), web player kreće tek kad se stream učita i prebacuje se na H.264/AAC kad preglednik ne može dekodirati datoteku (#60). Knjige, stripovi, audioknjige i fotografije još nisu uključeni.

## Live TV i DVR (branch `feat/livetv`, 2026-11-11)
Tuneri M3U (URL/datoteka) i HDHomeRun (SSDP + ručna adresa, lineup, broj tunera), XMLTV vodič (osvježavanje po rasporedu, preskakanje nepromijenjenih programa, slike kroz potpisani proxy), automatsko i ručno mapiranje, kategorije, favoriti po profilu; gledanje kao klizni HLS prozor (time-shift, ograničena veličina) s postojećom odlukom o transkodiranju i dijeljenjem tunera; DVR (pojedinačno, pravila serija nove/sve, padding, prioriteti, konflikti, `/recordings`, `.nfo`, biblioteka „Recordings", post-processing bez shella, opt-in retencija samo za vlastite snimke). Web (vodič, kanali, snimke, raspored, pravila, postavke, „On now" na Homeu), iOS i Android (kanali, now/next, gledanje, snimanje iz programa kanala; bez mreže vodiča), compat `/LiveTv/*` za TV aplikacije. Migracija 00031, D-041, `docs/LIVETV.md`.
- Testirano: Go unit (M3U i XMLTV s kodiranjima i zonama, konflikti, pravila serija, padding, argumenti post-processinga, putanje, lažni HDHomeRun), integracijski protiv Postgresa s lažnim M3U izvorom koji strima MPEG-TS iz ffmpeg testsrc-a: gledanje, dijeljeni tuner, time-shift prozor i premotavanje, ograničen buffer, prava kratka snimka koja preuzima tuner gledatelju, `.nfo`, post-processing, biblioteka, retencija; lažni HDHomeRun (lineup, remux bez probe, 503 → `tuners_busy`); compat kanali/programi/PlaybackInfo; Vitest (vodič, zakazivanje, pravila, Test tunera), Android unit.
- Nije provjereno: pravi IPTV provider i pravi HDHomeRun, SSDP otkrivanje na pravoj mreži, iOS samo kroz CI build, TV aplikacije s pravim klijentom, hardversko transkodiranje uživo.
- Sljedeće: Schedules Direct, comskip → segmenti reklama, gledanje snimke dok traje, mreža vodiča u aplikacijama.

## Preuzimanje za offline gledanje (branch `feat/offline`, PR #57, 2026-11-11)
- Server: poslovi preuzimanja (original ili MP4 u izvornoj, 1080p ili 720p kvaliteti), pretvaranje u pozadini s ograničenjem i čišćenjem cachea, potpisani linkovi s Range, sinkronizacija napretka „najnovije pobjeđuje" i povlačenje pristupa (D-040).
- Web: „Preuzmi original" i admin kartica u postavkama transkodiranja.
- iOS: pozadinski URLSession, offline biblioteka i player, automatski „sljedećih N", sinkronizacija. Samo CI (build i unit testovi), nije pokrenuto na uređaju.
- Android: WorkManager u prvom planu, nastavak preuzimanja, offline početni ekran i player, automatski „sljedećih N", sinkronizacija. Unit testovi (74) i release build prolaze; na emulatoru nije pokrenuto.
- Poznato: preuzimanja nisu vezana uz profil na uređaju; povlačenje pristupa vrijedi tek pri sljedećoj sinkronizaciji; jedna audio staza, bez slikovnih titlova.

## Zajedničko gledanje / SyncPlay (branch `feat/syncplay`, PR #53, 2026-11-11)
- Server: paket `syncplay` (grupe u memoriji, član = sesija pa preživi reconnect, queue, sinkroni start u budućnosti, pauza/seek/brzina, čekanje na bufferiranje s timeoutom 20 s, korekcija drifta sa servera brzinom ±5/10 % ili seekom), REST `/syncplay/*` + WebSocket `/api/v1/syncplay/socket` (D-036), dozvola `syncPlay`, pristup bibliotekama za svakog člana, rate limit po sesiji. Bez migracije.
- Compat: `/SyncPlay/*`, `/GetUtcTime`, `SyncPlayGroupUpdate`/`SyncPlayCommand` na `/socket` — TV aplikacije i Streamero klijenti u istoj grupi (samo simulirani klijent u testu).
- Web: „Gledajmo zajedno" na stranici naslova i u playeru (poveznica, članovi, „Čekamo: X", izlazak), `/together` i `/together/<id>`; HR/EN.
- iOS i Android: pridruživanje s popisa ili zalijepljenom poveznicom, praćenje play/pauza/seek/brzine s korekcijama; samo unit testovi + CI build, nisu isprobani na uređaju.
- Testovi: Go unit (stanja grupe, drift, rate limit), integracijski s dva klijenta preko pravog socketa + Postgres, compat integracijski, Vitest (engine + player + pozivnica), Android/iOS unit testovi sync enginea.
- Otvoreno: chat/reakcije, provjera sa stvarnim TV appom, e2e Playwright s dva preglednika.

## Utiskivanje titlova i prepoznavanje uvoda/odjavne špice (branch `feat/subs-burnin-intros`, 2026-11-11)
Slikovni titlovi (PGS, VobSub, DVB; ugrađeni i vanjski) utiskuju se u sliku kad ih klijent ne može nacrtati, s razlogom „subtitle burn-in" na nadzornoj ploči; ASS na zahtjev sa stilom; forced slikovni po zadanom; compat `SubtitleStreamIndex`/`Encode`. Novi zadatak „Prepoznaj uvode i odjavne špice" (fpcalc otisci po sezoni, crni kadrovi kao rezerva, inkrementalno, migracija 00025, D-035). Web player nudi slikovne titlove.
- Testirano s pravim medijima (ffmpeg/fpcalc u testu): utiskivanje PGS i ASS (ugrađeni i vanjski) s mjerenjem svjetline kadra prije/poslije, i nakon premotavanja; sintetička sezona sa zajedničkim uvodom, zajedničkom pjesmom odjavne špice i epizodom s crnom odjavnom špicom; integracijski protiv Postgresa (prednost ručnog/poglavlja, inkrementalnost, nova i zamijenjena epizoda; playback API, nadzorna ploča, compat `PlaybackInfo` i `/MediaSegments`).
- Nije provjereno: pravi Blu-ray/DVD remuxi, HW enkoderi s utiskivanjem (samo argumenti), prava serija (samo sintetička), iOS/Android/TV klijenti.
- Sljedeće: izbor slikovnih titlova u iOS/Android aplikacijama; editor segmenata; recap/preview iz zvuka.

## iOS paritet s Androidom (branch `feat/ios-parity`, PR #38, 2026-11-09)
Nativna iOS aplikacija dobila je sve ekrane Android aplikacije: Home/Search/Discover/Libraries/Me, filtre biblioteke, Discover sa zahtjevima, stranicu naslova, player (napredak, naredbe s nadzorne ploče, preskakanje uvoda, sljedeća epizoda, titlovi preko videa), glazbu s pozadinskim zvukom, Bazarr, račun i sve administratorske stranice.
- Provjera samo kroz CI (xcodebuild test + iphoneos build); na uređaju i protiv pravog servera još nije isprobano.
- Poznato: skok naprijed/natrag iz postavki ne primjenjuje se u AVKit kontrolama (isto kao na Androidu).
- Sljedeće: ručna provjera na uređaju (reprodukcija, glazba na zaključanom ekranu).

## Jedinstvena prijava (OIDC) i LDAP (branch `feat/sso-ldap`, 2026-11-11)
OpenID Connect (više providera, PKCE, state/nonce, JWKS rotacija, mapiranje claimova i grupa, povezivanje po potvrđenoj e-pošti ili iz „Moj račun", automatsko stvaranje računa) i LDAP/AD (servisni bind, escape filtera, memberOf ili pretraga grupa, StartTLS/LDAPS s provjerom certifikata). Web: gumbi na prijavi, Postavke → Prijava (Test connection za oba), povezani računi. iOS (ASWebAuthenticationSession) i Android (Custom Tabs) preko `streamero://oidc` + jednokratnog koda vezanog uz PKCE aplikacije. Migracija 00027, D-037, `docs/SSO.md`.
- Testirano: unit s lažnim OIDC providerom (loš potpis, HS256, kriva publika/izdavatelj, istekao token, nonce, azp, PKCE, rotacija ključeva, UserInfo), integracijski protiv Postgresa (stvaranje i povezivanje računa, ponovljeni state, login CSRF, app exchange, gašenje lozinke i oporavak, brisanje providera), LDAP protiv pravog OpenLDAP-a (CI service container), web (Vitest), Android unit (PKCE, API).
- Nije provjereno: iOS samo kroz CI build/testove; stvarni Authentik/Authelia/Entra/Google (samo po javnoj dokumentaciji); Active Directory.
- Sljedeće: ručna provjera s Authentikom na stabilnom serveru i na iOS uređaju (Feather build).

## DLNA server za televizore (branch `feat/dlna`, 2026-11-11)
DLNA / UPnP AV MediaServer (D-038): SSDP, opis uređaja, ContentDirectory (Browse s pagingom/sortiranjem, Search po naslovu, Samsung X_GetFeatureList/X_SetBookmark), ConnectionManager, DIDL-Lite s protocolInfo i DLNA zastavicama, plakati (JPEG_TN), titlovi (SRT, Samsung CaptionInfo.sec), byte-range i TimeSeekRange; profili LG/Samsung/Sony/generički; pretvaranje u MPEG-TS/MP3 kroz postojeće enkodere. Postavke → Server → „DLNA za televizore" (zadano isključeno; korisnik/profil u čije ime radi, naziv, jezik mapa). Roditeljska ograničenja profila primijenjena u DLNA stablu.
- Testirano: unit (SSDP parsiranje/odgovori/limiter/loopback, DIDL, profili, plan, seek zaglavlja, SOAP, ocjene → dob, `ProgressiveArgs`), integracijski protiv Postgresa (`TestDLNAIntegration`: opis, Browse/Search kroz SOAP, Range, HEAD s TimeSeekRange, bookmark → nastavi gledati, kids profil, isključeno → 404), web test postavki; živo u Dockeru s neovisnim UPnP klijentom (async-upnp-client + python-didl-lite): multicast otkrivanje, alive/byebye, cijelo stablo, paging/sort/search, Range, pretvoreni streamovi (AV1, HEVC 10-bit, HDR, multi-audio) provjereni ffprobeom, titlovi za LG i Samsung, SUBSCRIBE, bookmark.
- Nije provjereno: pravi televizori (LG, Samsung, Sony); procjena pozicije iz dohvaćenih bajtova samo unit testom.
- Poznato: UDP 1900 u composeu samo komentiran (multicast traži host mrežu ili macvlan); demo `.env` bi trebao dobiti `DLNA_HTTP_PORT=127.0.0.1:8788` kao i ostali portovi.
- Sljedeće: isprobati na vlasnikovom LG TV-u (host mreža na stabilnom serveru), po potrebi doraditi profile.

## TV aplikacije bez `/compat` (branch `feat/tv-clients`, 2026-11-10)
Compat API se služi i na korijenu adrese (D-032, `STREAMERO_COMPAT_ROOT`), s CORS-om za web TV aplikacije; `/socket` WebSocket (keepalive + poruka/stop s nadzorne ploče); odgovor na LAN otkrivanje na UDP 7359 (D-033, `STREAMERO_DISCOVERY`, `STREAMERO_DISCOVERY_URL`).
- Testirano: unit (otkrivanje, nesudaranje sa svim rutama web klijenta iz `router.tsx`, CORS, cookie se ne koristi na compat putanjama, rate limit na korijenu), integracijski protiv Postgresa (`TestCompatRootAndSocketIntegration`: prijava i pregled na korijenu, auth utičnice, keepalive, push naredbi).
- Nije provjereno u aplikacijama: Android TV na korijenskoj adresi / otkrivanje / socket, Samsung (Tizen) uopće. LG webOS ne radi: aplikacija učitava web klijent sa servera (D-034, odluka vlasnika).
- Sljedeće: provjera Android TV aplikacije u emulatoru na korijenskoj adresi i s otkrivanjem (host mreža); Samsung na pravom TV-u.

## Compat API za medijske klijente (branch `feat/compat-clients`, 2026-11-10)
`/compat` sada služi i Jellyfin-kompatibilne klijente (D-031): prijava imenom i Quick Connect (i za 2FA račune, odobrenje iz weba), pogledi, `/Items` s pagingom/sortiranjem/filterima, serije/sezone/epizode, slike, pretraga, resume/next up/latest, user data, PlaybackInfo (direct play ili HLS), stream, titlovi, izvještaji o reprodukciji, media segmenti. Migracija 00024 sprema profil/razinu/bit dubinu videa iz ffproba.
- Provjereno sa službenom Android TV aplikacijom 0.19.10 u emulatoru (Android TV 12, x86): otkrivanje servera, Quick Connect za 2FA korisnika, biblioteke, stranica serije s „Next up", favorit, direct play, HLS transcode (H.264 4:4:4 koji emulator ne dekodira), napredak u bazi.
- Nije provjereno: Infuse, Swiftfin, Kodi add-on, Samsung/LG (samo po javnoj dokumentaciji).
- Poznato: nema `/socket` (WebSocket) → nema daljinskog upravljanja; DisplayPreferences se ne pamte; datoteke skenirane prije 00024 nemaju profil dok se ne promijene.

## Uvoz iz Jellyfina (branch `feat/jellyfin-import`, 2026-11-10)
Postavke → Uvoz: admin upiše adresu i API ključ starog servera, poveže korisnike (po imenu, može promijeniti/preskočiti), doda preslikavanje mapa i pokrene probni prolaz ili uvoz (River job `import.run`). Prenosi pogledano, broj i datum gledanja, poziciju, favorite i popise; ponovni uvoz ne pravi duplikate i ne pregazi noviji napredak (D-024).
- Testirano: unit (ticks → ms, uparivanje, spajanje/idempotentnost, povezivanje korisnika, lažni Jellyfin HTTP server), integracijski protiv Postgresa (cijeli uvoz dvaput, API s ključem koji se ne vraća), web test.
- Nije provjereno protiv pravog Jellyfina. Ne uvoze se računi ni kolekcije.

## v0.9.1 (2026-11-10)
TV aplikacije rade s čistom adresom servera (bez `/compat`), automatsko pronalaženje servera u kućnoj mreži (UDP 7359), `/socket` za poruke i zaustavljanje s nadzorne ploče, CORS za Samsung aplikaciju (#51). LG webOS i dalje ne radi (D-034).

## v0.9.0 (2026-11-10)
Backup i vraćanje (#46), uvoz povijesti, napretka, favorita i playlista iz Jellyfina (#45), Android TV (#47), kompatibilni sloj za Infuse, Swiftfin, Kodi i TV aplikacije s Quick Connect prijavom (#48), sigurnosne sitnice i uklonjena imena iz koda (#44), ključ uvoza izvan backupa (#49).

## v0.8.3 (2026-11-10)
iOS: slike više ne šire ekran preko ruba, a nakon ponovne instalacije i dodavanja servera traži se prijava (#41). Naslovi se prikazuju točno kako ih daje TMDb, umjesto velikim slovima. Spremanje TMDb ključa ponovno pokušava naslove kojima je pretraga propala. Verzija je samo u postavkama (#42).

## v0.8.2 (2026-11-09)
iOS aplikacija s gotovo punim paritetom (#38) i ispravljeni oblici množine na Androidu (#39). iOS je preveden i prošao unit testove na CI-ju, ali još nije pokrenut na uređaju.

## v0.8.1 (2026-11-09)
Popravak premotavanja (#33), upit za TMDb ključ (#34), Bazarr titlovi (#35), nativna Android aplikacija (#36). iOS `.ipa` još nema puni paritet s webom.

## Android aplikacija (branch `feat/android`, 2026-11-09)
Nativna Kotlin/Compose aplikacija u `android/`; CI `android.yml`, `.apk` ide u release uz `.ipa`.
- Provjereno na emulatoru (release build, R8) protiv lokalnog servera: spajanje, prijava, admin vidi administraciju, običan korisnik ne vidi, prazan Home.
- Još nije provjereno na emulatoru: reprodukcija videa i glazbe te Bazarr protiv pravog servera.
- Poznato: bez secreta `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD` i `ANDROID_KEY_ALIAS` CI potpisuje debug ključem.
- Sljedeće: iOS nativni paritet s webom (biblioteke/filtri, pretraga, Discover, kolekcije, glazba, račun, sve postavke, admin samo za admine, Bazarr, poglavlja).

## v0.8.0 (2026-11-09)
- Redizajn „Kino platno" (D-019), lokalni .nfo i slike pokraj medija, kartice i filteri biblioteke, poglavlja na stranici naslova.
- Postavke: transkodiranje (VAAPI/QSV/NVENC), nastavak gledanja, brendiranje, logovi, aktivnost; po profilu kontrole, kvaliteta, izgled titlova, sekcije početne.
- Moj račun (ime, e-mail, jezik, slike računa i profila); engleski kao glavni jezik.
- Discover sa zahtjevima preko Seerra i kalendarom iz Radarra/Sonarra; ugrađena proširenja (D-020).
- Odabir verzije/zvuka/titlova prije puštanja; poruka i zaustavljanje s nadzorne ploče.
- Glazbena biblioteka i audio player.
- Poznato: iOS app još nema glazbu, račun ni Discover; Discover je testiran s lažnim Seerrom.
- Sljedeće: iOS (glazba, Discover, račun), fotografije, Chromecast, LDAP/OIDC.

## Redizajn „Kino platno" (D-019)
- Web i iOS prepisani na novi vizualni smjer: slika filma preko cijelog ekrana, široki naslovi, pravokutne kontrole, tekstualna navigacija.
- Server uvozi lokalne postere/pozadine/logotipe iz mapa filmova (`metadata.LocalArtwork`); demo ima službene CC-BY postere Blender filmova.
- Bogatiji sadržaj i funkcije (PR „richer library"): čitanje Kodi .nfo datoteka (opis, žanrovi, studiji, ekipa, set → kolekcija), studiji s TMDb-a (migracija 00019), poglavlja sa sličicama, poveznice IMDb/TMDb, kartice biblioteke (Sve, Preporučeno, Žanrovi, Kolekcije, Moja lista), filteri (status, desetljeće, A–Z), prikaz posteri/popis.
- Postavke: transkodiranje (VAAPI/QSV/NVENC, preset, kvaliteta, zvuk, segmenti, HDR→SDR, najviše istovremenih), nastavak gledanja (pragovi), brendiranje (poruka na prijavi, CSS), logovi (serve + worker preko Valkeyja), aktivnost; po profilu: kontrole (skokovi, uvod/špica, odbrojavanje), kvaliteta (max rezolucija i bitrate, provodi server), izgled titlova, sekcije početne.
- Demo: 9 CC-BY Blender filmova i serija Caminandes (3 epizode) s .nfo opisima na hrvatskom i službenim posterima.
- Sljedeće: odabir verzije/zvuka/titlova prije reprodukcije, zaustavi stream / poruka s nadzorne ploče, glazbena biblioteka s audio playerom.

## v0.6.0 i nakon toga (PR #16–#23)
- Netflix raspored (top nav, „Tko gleda?", billboard s utišanim pregledom, redovi, Top 10, TV navigacija strelicama), logotipi i fotografije glumaca s TMDb-a, sličice iz videa, demo s CC BY Blender filmovima.
- Popravak: ponovna skeniranja/osvježavanja više se ne odbacuju (River uniqueness).
- Admin: nadzorna ploča, zakazani zadaci, obavijesti (Discord/ntfy/Gotify/webhook).
- Postavke reprodukcije po profilu, Next Up, sljedeća epizoda s odbrojavanjem.
- Kolekcije (TMDb + ručne), playliste, Identify, uređivač metapodataka s lockom.
- Analiza medija: poglavlja, intro/credits (poglavlja + Chromaprint), trickplay; novi web player s vlastitim kontrolama; iOS preskakanje uvoda i sljedeća epizoda.
- **Izdanje v0.6.0** (GHCR + .ipa). Sve nakon PR #20 ide u v0.7.0.

## Dizajn i brand (PR #13)
- Znak „razdvojeni play" (`brand/mark.svg`), favicon, PWA/Apple touch ikone, iOS AppIcon + AccentColor + AppMark (D-017).
- Web redizajn: tokeni „projection room" (topla crna, jantarni akcent), Geist + Instrument Serif (self-host), lucide ikone, sidebar s bibliotekama, plutajući top bar, hero, horizontalni redovi, kartice „nastavi gledati", stranica naslova (backdrop hero, sezone, epizode sa stillovima, glumci, tehnički podaci), tipografski placeholder posteri, fade-in slika, reduced motion.
- iOS redizajn: isti tokeni, New York serif za naslove, hero, redovi, tabovi Početna/Pretraga, nova stranica naslova (favorit/odgledano), login i dodavanje servera sa znakom.
- API: `hasBackdrop` na ItemSummary.
- Compat: `/System/Configuration` i `/Notifications/Admin` (test veze u Sonarr 4.0.20+ i Radarr 6.x), PR #12.

## P4b — *arr ekosustav (branch `phase/P4b-arr`)
- Compat API pod `/compat` (D-016): prijava, korisnici, biblioteke, stavke s ProviderIds/4K, sezone/epizode, latest, slike, `/Auth/Keys`, `/Library/Media/Updated`, „Play on" preusmjerenje.
- Provjereno sa stvarnim Jellyseerr 2.7.3 (prijava, sync biblioteka, film dostupan, serija djelomično po sezonama) i Radarr 5.26 (Emby/Jellyfin veza). Bazarr radi preko watchera.
- Web: Postavke → Integracije s gotovim vrijednostima; `docs/INTEGRATIONS.md`.

## P4 — pregledavanje, prvi dio (branch `phase/P4-browse`)
- Pretraga (tipfeleri preko pg_trgm word_similarity, glumci/redatelji), favoriti, odgledano/neodgledano (serija/sezona → sve epizode), „Nedavno dodano", „Favoriti", filtri po žanru i neodgledano, sortiranje po ocjeni; migracija 00012.
- Testovi: API integracijski (tipfeler, osobe, wildcards, žanr, kaskada odgledano, favoriti), EXPLAIN s realnom veličinom tablice, Vitest 31, Playwright 11.
- Još u P4: playliste, kolekcije, stranice glumaca, prilagodba početne.

## P5 — reprodukcija, prvi dio (branch `phase/P5-playback`)
- `POST /items/{id}/playback`: direct play / remux / transcode po profilu uređaja i dozvolama; potpisani URL-ovi (D-015); `/stream/file` (Range), `/stream/hls` (fMP4 HLS iz ffmpega: copy ili x264, HDR→SDR tonemap, skaliranje), `/stream/sub` (WebVTT); gašenje neaktivnih sesija.
- Napredak po profilu (migracija 00011), nastavak gledanja, „Nastavi gledati", odgledano iznad 90 %.
- Web player (hls.js / nativni HLS), izbor zvuka i podnaslova, gumbi Pokreni / Nastavi / Od početka, trake napretka.
- Testovi: unit (potpisi, odluka), API integracijski (Range, potpis, napredak po profilu), Vitest 29, Playwright 11 (pravi ffmpeg transcode s potpisanim segmentima); HDR tonemap provjeren ručno.
- Još u P5: HW ubrzanje (VA-API/QSV/NVENC), burn-in bitmap podnaslova, premotavanje izvan već transkodiranog dijela, bitrate/kvaliteta izbornik, trickplay (P6).

## P3 — metapodaci, prvi dio (branch `phase/P3-metadata`, uključuje P2 watch)
- TMDb ključ (v3 ili v4) u Postavke → Metapodaci (provjeren prije spremanja, nikad se ne vraća), jezik (hr-HR, fallback EN opis) i država (dobne oznake).
- Automatski nakon scana: podudaranje (TMDb ID iz imena → IMDb /find → pretraga naslov+godina), naslov/izvorni naslov, opis, slogan, žanrovi, ocjena, dobna oznaka, premijera, glumci, režija/scenarij; sezone i epizode (nazivi, opisi, datumi, stillovi).
- Posteri/pozadine/stillovi u `/cache/images`, `GET /items/{id}/images/{type}`; čišćenje osirotjelih slika; migracija 00010.
- Web: posteri u mreži, hero s pozadinom, opisom i ulogama; Postavke → Metapodaci; „Osvježi metapodatke" po biblioteci.
- Testovi: integracijski s lažnim TMDb serverom (film, ID iz imena, serija/epizode, bez podudaranja, re-scan čuva naslov, čišćenje slika), Vitest 26, Playwright 10.
- Još u P3: Identify (ručni odabir), zaključavanje polja, slike osoba, AVIF/WebP + blurhash, OMDb/Fanart/NFO, automatske kolekcije.

## P2 — drugi dio (branch `phase/P2-watch`)
- Praćenje mapa (fsnotify, rekurzivno, nove podmape, debounce 15 s) → automatski scan; periodični scan svih biblioteka svakih 6 h (NFS/SMB); `.streameroignore`/`.ignore` marker.
- Odluka korisnika: TMDb API ključ se upisuje u Postavke aplikacije (baza), ne u `.env`.

## P2 — prvi dio (PR #5, izdanje v0.2.0)
- Biblioteke (filmovi, serije, mješovito; više mapa), API + admin UI s preglednikom mapa; svaka putanja kroz `library.ResolveSafePath` unutar `STREAMERO_MEDIA_ROOTS`; migracija 00009.
- Skener kao River job: parsiranje imena (naslov/godina, release tagovi, provider ID, verzije, extras/sample/trailer, skrivene/NAS mape), epizode (SxxEyy, multi, NxNN, specijali, daily, anime absolute), ffprobe (rezolucija, kodek, HDR10/HLG/DV, zvuk, podnaslovi) + vanjski podnaslovi (dijele ih sve verzije), paralelno probanje, inkrementalno; nedostupna mapa = scan pada bez brisanja.
- Web: biblioteke na početnoj, mreža s poretkom i straničenjem, stranica stavke (verzije, tehnički podaci, sezone/epizode).
- Ovaj server je samo dev (demo.streamero.net): `MEDIA_PATH=../data/media` (prazno), test mediji na `/fixtures`. Stabilna verzija ide na zaseban server preko `deploy/docker-compose.yml` + GHCR imagea (DEPLOY.md).
- Testovi: golden parseri, ffprobe parser, integracijski scan (filmovi, serije, promjene, offline mapa), EXPLAIN indeksi, API (putanje, dozvole), Vitest 25, Playwright 10 (pravi scan s ffprobeom).

## P1 — gotovo (PR #4)
- API (spec → ogen): `/setup`, `/auth/{login,logout,me,password}`, `/users`, `/sessions`, `/settings/server`; migracija 00002 (users, sessions, audit_log); D-012.
- Setup wizard sa setup kodom iz loga, login/odjava (cookie za web, Bearer za aplikacije), lockout, upravljanje korisnicima (admin, onemogući, lozinka, brisanje, zaštita zadnjeg admina), uređaji s trenutnim opozivom, promjena lozinke (odjavljuje ostale uređaje), ime servera.
- Web: setup, prijava, ljuska aplikacije, Postavke (Moj račun, Uređaji, Server, Korisnici, API ključevi), HR/EN.
- iOS: prijava lozinkom (+ 2FA korak) i Quick Connectom, token u Keychainu (preživi Feather resign), obnova sesije pri pokretanju, odjava; iOS CI build + testovi zeleni.
- Pravi IP klijenta iza proxyja (`STREAMERO_TRUSTED_PROXIES`, D-014), rate limit po IP-u (`STREAMERO_AUTH_RATE_LIMIT`, zadano 20/min, burst 10) na prijavi/setupu/Quick Connectu/pozivnicama/PIN-u, IP u audit logu.
- Profili (do 6 po računu): ime, boja, PIN (5 pogrešnih → 5 min), dječji profil (bez postavki/admina, izlaz traži lozinku računa), dobna granica (model; provedba s ocjenama u P3); profil po sesiji; migracija 00008.
- Pozivnice: admin napravi poveznicu (rok, broj računa, dozvole), primatelj napravi običan račun i odmah je prijavljen; potrošnja je atomična; migracija 00007.
- Dozvole po korisniku (model + API + UI, zadane vrijednosti za nove ključeve; provedba po značajci) i max broj prijavljenih uređaja (prijava preko limita odjavljuje najdulje neaktivan uređaj); migracija 00006.
- Quick Connect: novi uređaj prikaže 6-znamenkasti kod (5 min), prijavljeni korisnik ga odobri u Postavkama → uređaj se prijavi tim računom; tajna za polling samo jednom; migracija 00005.
- 2FA (TOTP, RFC 6238 bez vanjske biblioteke; QR + otpauth link; 10 jednokratnih kodova za oporavak; zaštita od ponovne upotrebe koda; pogrešni kodovi ulaze u lockout; admin može poništiti 2FA); migracija 00004.
- API ključevi (`sk_…`, Bearer; rade s pravima admina koji ih je napravio, gase se ako izgubi admin prava), preimenovanje uređaja; migracija 00003.
- Testovi: Go unit + integracijski protiv Postgresa (setup, cookie/bearer, revoke, lockout, zadnji admin, promjena lozinke), Vitest 18, Playwright 9 (setup, korisnici, uređaji, Quick Connect, pozivnice, lockout, odjava — na svježem stacku).

## Sljedeći koraci
1. Redoslijed (dogovoren s korisnikom): iOS player (P7) → ostatak P2/P3/P5 → P6 → P8–P14. P2 ostatak: watch status kroz preimenovanje (inode/hash), golden testovi do 300+, `seed-big-library.sh` + 50k benchmark, provider ID na sezonama/epizodama, verzije epizoda.
2. Instalirati `streamero-ipa` (artefakt iOS CI-ja) kroz Feather, spojiti se na https://demo.streamero.net i prijaviti (lozinka / Quick Connect).
3. Dizajn sljedeće: slike glumaca (TMDb profile) + stranica osobe, logo naslova (clearlogo), vlastite kontrole playera na webu, detaljniji redizajn ekrana postavki.
4. *arr ekosustav je faza P4b (D-011).

## Poznati problemi / blokeri
- `server-image.yml` ručno pokrenut s feature brancha nema tagova (samo `main`/`v*`) i pao bi; multi-arch (arm64) image još nije buildan.
- Claude na serveru nema sudo: nginx/certbot/apt promjene radi korisnik.
- `scripts/seed-big-library.sh` dolazi u P2 (treba scanner/shemu).
