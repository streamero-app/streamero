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
