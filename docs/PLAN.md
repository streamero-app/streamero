# Streamero — plan izgradnje za Claude Code

> Self-hosted media server (alternativa Jellyfinu) + web klijent + iOS app (.ipa za Feather).
> Brand: SCYDE. Deploy: Docker Compose. Kod: GitHub. Build/dev: vlastiti server (kao Mediarr).
> Referentna točka za parity: **Jellyfin 12.1** (rujan 2026). Kompletna lista featurea: `docs/FEATURES.md`.

---

## 0. Pravila igre (pročitaj prije svega)

1. **Clean-room.** Jellyfin je GPL-2.0. Ne kopiramo njegov kod, ne prevodimo ga redak po redak, ne gledamo njegove izvorne datoteke dok pišemo ekvivalent. Smijemo koristiti: javnu dokumentaciju, javnu OpenAPI specifikaciju (za opcionalni compat sloj), konvencije imenovanja datoteka (one su de facto standard Kodi/Plex/Jellyfin). Isto vrijedi za Swiftfin (MPL-2.0) i Jellyfin-web.
2. **Ime i brand:** nigdje „Jellyfin" u UI-ju, ikonama, bundle ID-u. Sve je Streamero / SCYDE.
3. **Licenca Streamera:** dok je privatno — „All rights reserved". Za kasniji open-source release preporuka **AGPL-3.0** (kao Immich).
4. **FFmpeg:** koristimo `jellyfin-ffmpeg` binarne buildove (GPL, javni izvor — samo ih pozivamo kao vanjski proces, ne linkamo) ili BtbN static build. U `THIRD_PARTY.md` navesti licence.
5. Kod, komentari, commit poruke, API: **engleski**. UI: **i18n**, jezici HR + EN od prvog dana.

---

## 1. Arhitektura

```
                ┌──────────────── iOS app (SwiftUI, .ipa → Feather)
                │                 AVPlayer (HLS) + MPVKit fallback
 Internet ──► reverse proxy (Caddy/NPM/Traefik, TLS)
                │
                ▼
        ┌──────────────────────────────────────────────────────┐
        │ streamero (Go)                                       │
        │  ├─ HTTP API /api/v1 (REST, OpenAPI 3.1)             │
        │  ├─ WebSocket /api/v1/ws (sesije, SyncPlay, eventi)  │
        │  ├─ Web klijent (React build, embed u binarij)       │
        │  ├─ Stream: direct play / HLS remux / HLS transcode  │
        │  │    └─ ffmpeg procesi (HW accel)                   │
        │  └─ Bonjour/mDNS oglašavanje (_streamero._tcp)       │
        └─────────┬───────────────────────┬────────────────────┘
                  │                       │ jobs (River queue u Postgresu)
                  ▼                       ▼
          PostgreSQL 17            streamero-worker (isti image, druga komanda)
          + pgvector               scanner, metadata, slike, trickplay,
          + pg_trgm                chapter images, fingerprint (intro),
                  ▲                podnaslovi, backup, AI embeddings
                  │
           Valkey (cache, pub/sub između servera i workera, rate limit)

  Opcionalni sidecari (compose profili): whisper (faster-whisper), ollama (vanjski),
  comskip, prometheus + grafana
```

### Zašto ovaj stack
| Dio | Izbor | Razlog |
|---|---|---|
| Server | **Go 1.25+** | jedan statički binarij, odlična konkurentnost za stream/ffmpeg procese, mali RAM, brz start; lako za Docker multi-arch |
| HTTP | `chi` router + `ogen` (codegen iz OpenAPI) | spec-first: OpenAPI je izvor istine za server, web i iOS |
| DB | **PostgreSQL 17** + `sqlc` + `goose` migracije | Jellyfinov najveći problem su performanse velikih biblioteka na SQLiteu — Postgres + pravi indeksi; pgvector za AI pretragu; pg_trgm za fuzzy search |
| Queue | **River** (Postgres-backed job queue) | bez dodatnog brokera, transakcijski siguran, retry, prioriteti, cron |
| Cache/pubsub | **Valkey 8** | session cache, WebSocket fan-out između instanci, rate limit |
| Slike | **libvips** (govips) | resize/AVIF/WebP brzo i s malo memorije |
| Mediji | ffmpeg/ffprobe 8.x, chromaprint (`fpcalc`), libass preko ffmpega | |
| Web | **React 19 + Vite + TypeScript + TanStack Router/Query + Tailwind v4 + Radix UI** | brzo, tipizirano, generirani API klijent |
| Web player | **hls.js** + native `<video>` (Safari), Shaka kao opcija za DV/HEVC edge caseove; `libass-wasm` (JASSUB) za ASS | |
| iOS | **Swift 6, SwiftUI, iOS 17+**, XcodeGen, swift-openapi-generator, Nuke (slike), MPVKit, Google Cast SDK | XcodeGen = projekt kao `project.yml` (Claude Code ga može uređivati na Linuxu, nema `.pbxproj` konflikata) |
| CI | GitHub Actions | server/web image na GHCR; iOS na `macos-15` (GitHub-hosted) |

> Ako Mediarr već koristi drugačiji backend stack i želiš isti — zamijeni u ovoj tablici prije P0, ostatak plana vrijedi.

---

## 2. Struktura repozitorija (monorepo)

```
streamero/
├── CLAUDE.md                      # upute za Claude Code (pravila, komande)
├── README.md
├── THIRD_PARTY.md                 # licence ffmpeg, MPVKit, itd.
├── api/
│   └── openapi.yaml               # IZVOR ISTINE za API
├── server/
│   ├── cmd/streamero/main.go      # subkomande: serve | worker | migrate | admin
│   ├── internal/
│   │   ├── api/                   # ogen handleri (generirano + implementacija)
│   │   ├── auth/                  # lozinke (argon2id), sesije, API ključevi, TOTP, OIDC, LDAP, QuickConnect, invites
│   │   ├── users/                 # korisnici, profili, dozvole, parental
│   │   ├── library/               # biblioteke, item model, upiti, user data
│   │   ├── scanner/               # walk, fsnotify, naming parser, ffprobe, ignore pravila, verzije, extras
│   │   ├── naming/                # parseri imena (film, serija, anime, glazba, knjige) + golden testovi
│   │   ├── metadata/              # orchestrator + providers/{tmdb,tvdb,omdb,fanart,musicbrainz,audiodb,openlibrary,googlebooks,comicvine,anilist,nfo,embedded}
│   │   ├── images/                # cache, resize, blurhash, generiranje postera
│   │   ├── playback/              # device profile → odluka (direct/remux/transcode), media info
│   │   ├── transcode/             # ffmpeg command builder, HW accel, tonemap, sesije, throttling, cleanup
│   │   ├── hls/                   # master/variant playlisti, segmenti, seek restart
│   │   ├── subtitles/             # ekstrakcija, konverzija (→ WebVTT/ASS), burn-in odluka, upload, download, sync
│   │   ├── trickplay/             # sprite sheetovi
│   │   ├── segments/              # media segments, chromaprint intro/outro detekcija, editor API, EDL
│   │   ├── music/ books/ photos/  # specifična logika tipova
│   │   ├── livetv/                # tuneri (HDHR, M3U), EPG (XMLTV, SchedulesDirect), DVR, timeri
│   │   ├── syncplay/              # Watch Party
│   │   ├── sessions/              # aktivne sesije, remote control, WebSocket hub
│   │   ├── tasks/                 # River jobovi + scheduler + napredak
│   │   ├── notify/                # webhooks, Discord, Telegram, ntfy, email, newsletter
│   │   ├── integrations/          # trakt, anilist, opensubtitles, radarr/sonarr, requests, jellyfin-import
│   │   ├── ai/                    # ollama klijent, embeddings, whisper klijent, prijevod podnaslova
│   │   ├── backup/                # pg_dump + config snapshot, restore
│   │   ├── discovery/             # mDNS/Bonjour
│   │   ├── metrics/               # Prometheus
│   │   └── config/                # env + DB settings
│   ├── db/
│   │   ├── migrations/            # goose SQL migracije
│   │   └── queries/               # sqlc .sql upiti
│   └── testdata/                  # naming golden fixtures
├── web/
│   ├── src/{app,routes,components,features,player,i18n,api}
│   └── e2e/                       # Playwright
├── ios/
│   ├── project.yml                # XcodeGen
│   ├── Streamero/                 # app target
│   │   ├── App/ Core/ (API, Auth, Storage, Downloads, Discovery)
│   │   ├── Features/ (Home, Library, Detail, Search, Player, Music, Books, Photos, LiveTV, WatchParty, Settings, Downloads)
│   │   ├── Player/ (AVPlayerEngine, MPVEngine, PlayerViewModel, Subtitles, Trickplay)
│   │   ├── DesignSystem/
│   │   └── Resources/ (Localizable.xcstrings HR+EN, Assets)
│   └── StreameroTests/
├── deploy/
│   ├── docker-compose.yml
│   ├── compose.nvidia.yml  compose.vaapi.yml  compose.qsv.yml
│   ├── compose.extras.yml         # whisper, prometheus, grafana, comskip
│   ├── .env.example
│   ├── Dockerfile                 # multi-stage: web build → go build → runtime s ffmpeg
│   └── grafana/streamero.json
├── feather/
│   └── source.json                # AltStore/Feather repo source (auto-update appa)
├── scripts/
│   ├── gen-fixtures.sh            # ffmpeg generira test medije (h264, hevc10, av1, HDR, multi-audio, ass/srt)
│   ├── gen-api.sh                 # ogen + openapi-typescript + swift-openapi-generator
│   └── seed-big-library.sh        # sintetička biblioteka od 50k stavki za perf testove
├── docs/
│   ├── PLAN.md  FEATURES.md  PROGRESS.md  DECISIONS.md  NAMING.md  API.md  DEPLOY.md  IOS.md
└── .github/workflows/
    ├── ci.yml                     # lint + test server/web, generirani kod up-to-date
    ├── server-image.yml           # buildx amd64+arm64 → ghcr.io/<owner>/streamero
    ├── ios.yml                    # unsigned .ipa artefakt
    └── release.yml                # tag v* → image + .ipa u GitHub Release + update feather/source.json
```

---

## 3. Docker Compose (ciljno stanje)

`deploy/docker-compose.yml`:

```yaml
name: streamero

x-common: &common
  image: ghcr.io/${GH_OWNER:-byscyde}/streamero:${STREAMERO_VERSION:-latest}
  restart: unless-stopped
  env_file: .env
  environment:
    TZ: ${TZ:-Europe/Zagreb}
    STREAMERO_DB_URL: postgres://streamero:${DB_PASSWORD}@db:5432/streamero?sslmode=disable
    STREAMERO_VALKEY_URL: redis://valkey:6379/0
  volumes:
    - ${CONFIG_PATH:-./data/config}:/config
    - ${CACHE_PATH:-./data/cache}:/cache
    - ${MEDIA_PATH:-/mnt/media}:/media:ro
  depends_on:
    db: { condition: service_healthy }
    valkey: { condition: service_healthy }

services:
  streamero:
    <<: *common
    command: ["serve"]
    ports:
      - "${HTTP_PORT:-8787}:8787"
    volumes:
      - ${CONFIG_PATH:-./data/config}:/config
      - ${CACHE_PATH:-./data/cache}:/cache
      - ${MEDIA_PATH:-/mnt/media}:/media:ro
      - ${RECORDINGS_PATH:-./data/recordings}:/recordings
      - type: tmpfs              # transcode segmenti u RAM-u
        target: /transcode
        tmpfs: { size: 8g }
    healthcheck:
      test: ["CMD", "/app/streamero", "healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5

  worker:
    <<: *common
    command: ["worker"]
    # koristi iste GPU override datoteke za trickplay/HW dekod

  db:
    image: pgvector/pgvector:pg17
    restart: unless-stopped
    environment:
      POSTGRES_USER: streamero
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: streamero
    volumes:
      - ${DB_PATH:-./data/postgres}:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U streamero"]
      interval: 10s
      retries: 5
    shm_size: 256mb

  valkey:
    image: valkey/valkey:8-alpine
    restart: unless-stopped
    command: ["valkey-server", "--save", "", "--appendonly", "no"]
    healthcheck:
      test: ["CMD", "valkey-cli", "ping"]
      interval: 10s
```

GPU overridei (primjer `compose.vaapi.yml` za Intel/AMD, npr. RX 6600):
```yaml
services:
  streamero:
    devices: ["/dev/dri:/dev/dri"]
    group_add: ["${RENDER_GID:-105}"]
  worker:
    devices: ["/dev/dri:/dev/dri"]
    group_add: ["${RENDER_GID:-105}"]
```
`compose.nvidia.yml`: `deploy.resources.reservations.devices: [{driver: nvidia, count: all, capabilities: [gpu, video, compute, utility]}]`.

Pokretanje: `docker compose -f docker-compose.yml -f compose.vaapi.yml up -d`.

**Bonjour discovery** zahtijeva da mDNS paketi dođu do LAN-a: ili `network_mode: host` za `streamero` servis (opcija u `.env`, dokumentirano), ili zaseban mali `discovery` sidecar s host networkom. Ručni URL uvijek radi.

**Dockerfile** (multi-stage): `node:22` (web build) → `golang:1.25` (embed web dist, `CGO_ENABLED=1` zbog libvips) → runtime `debian:trixie-slim` + `jellyfin-ffmpeg8` paket (ima VA-API/QSV/NVENC/Vulkan/OpenCL), `libvips`, `chromaprint-tools`, `mesa-va-drivers`, `intel-media-va-driver-non-free` (amd64). Non-root user `streamero` (UID 1000). Port 8787.

---

## 4. Podatkovni model (sažetak — detalji u migracijama)

- `libraries` (id, name, type, options jsonb), `library_paths`
- `items` — jedna tablica za sve tipove: `id uuid, library_id, type (movie|series|season|episode|music_artist|music_album|audio|music_video|video|photo|photo_album|book|audiobook|folder|collection|playlist|tv_channel|tv_program|recording|person|genre|studio…), parent_id, series_id, season_id, name, sort_name, original_title, overview, tagline, premiere_date, end_date, production_year, index_number, parent_index_number, index_number_end, official_rating, rating_score, community_rating, critic_rating, runtime_ticks, status, path, is_folder, is_virtual (missing/unaired), date_created, date_modified, extra_type, owner_id (za extras), version_group_id, metadata_locked_fields text[], search tsvector, embedding vector(768)`
- `media_sources` (file po verziji: path, container, size, bitrate, mtime, inode, version_name) + `media_streams` (video/audio/subtitle/attachment: codec, profile, level, bit_depth, color_transfer, color_primaries, dv_profile, hdr_format, width, height, fps, channels, layout, language, title, is_default, is_forced, is_external, external_path)
- `external_ids` (item, provider, value), `images` (item, type, index, path, width, height, blurhash, source), `chapters`, `media_segments` (item, type, start, end, source, confidence), `trickplay_info`
- `people` + `item_people` (role, kind, sort_order); `item_genres`, `item_studios`, `item_tags`
- `users`, `profiles`, `user_permissions`, `parental_settings`, `user_library_access`, `devices`, `sessions` (hash tokena), `api_keys`, `totp`, `webauthn_credentials`, `invites`, `quick_connect_requests`
- `user_item_data` (profile_id, item_id, played, play_count, position_ticks, last_played, favorite, rating, likes, hidden, audio_stream_index, subtitle_stream_index) — **ključ performansi**: indeksi za Continue Watching i Next Up; `next_up` računati upitom s indeksom na (profile_id, series_id, last_played), ne u memoriji
- `playlists` + `playlist_items` (row po stavci, position), `collections` + `collection_items`, `smart_rules`
- `livetv_tuners`, `livetv_channels`, `epg_programs`, `timers`, `series_timers`, `recordings`
- `activity_log`, `audit_log`, `settings` (key/jsonb), `webhooks`, `playback_events` (za statistike), `requests`

ID-evi: UUIDv7 (sortabilni). Vrijeme: `timestamptz`. Trajanja u milisekundama (ne .NET ticks) — compat sloj konvertira.

---

## 5. Ključni podsustavi — kako ih napraviti dobro

### 5.1 Scanner
- Walk po putanjama → za svaku datoteku `(path, size, mtime, inode)`; usporedba s DB-om → samo promjene idu u pipeline (`probe → resolve → match → metadata → images → trickplay…` kao River jobovi).
- fsnotify s debounceom (npr. 5 s) i coalescingom po direktoriju; za mrežne mountove periodični scan.
- Resolver po tipu biblioteke koristi `internal/naming` (čisti parseri, 100 % unit-testirani golden fixtureima: stotine stvarnih imena filmova/serija/anime/glazbe). Pravila zapisati u `docs/NAMING.md`.
- Grupiranje verzija: isti folder + isti baza-naziv + `- 1080p`, `- Director's Cut` sufiksi; kod epizoda samo kad je parse siguran.
- Rename/move: match po inode+size ili po (provider ID + trajanje) → zadrži user data.

### 5.2 Playback odluka
Ulaz: `DeviceProfile` (klijent šalje pri `POST /playback/info`) + `MediaSource` + korisnička ograničenja (bitrate, dozvole) + mreža (LAN/WAN).
Izlaz: `PlaybackPlan { method: direct_play|remux|transcode, container, video: copy|encode(codec, w, h, bitrate, tonemap), audio: copy|encode(codec, ch, bitrate), subtitle: external_vtt|embedded|burn_in|client_render, reasons: [] }`.
Napisati kao **čistu funkciju** s tablicom testova (iOS AVPlayer profil, Safari, Chrome, Firefox, Chromecast, MPV profil).

### 5.3 Transkodiranje / HLS
- Master playlist → jedna varijanta (ABR opcionalno kasnije). Segmenti fMP4 (HEVC/AV1/DV) ili TS (H.264 legacy), 4–6 s, keyframe-aligned (`-force_key_frames`).
- Seek izvan generiranog područja → kill ffmpeg, restart od najbližeg segmenta (`-ss` prije `-i` + `-output_ts_offset`).
- Throttling: pauziraj ffmpeg (SIGSTOP/SIGCONT) kad je N segmenata ispred playheada; brisanje segmenata iza.
- Transcode sesija vezana uz playback sesiju; heartbeat; ubijanje nakon 60 s bez zahtjeva.
- `transcode/hwaccel`: builder filter grafova po metodi (vaapi, qsv, nvenc, amf, videotoolbox, rkmpp), uklj. tonemap (`tonemap_opencl`, `tonemap_vaapi`, `libplacebo` za Vulkan), deinterlace, scale, burn-in podnaslova (`subtitles`/`overlay` HW).
- Svaki ffmpeg poziv logirati u zaseban log (vidljiv u adminu).

### 5.4 Intro/credits detekcija (ugrađeno — Jellyfin treba plugin)
- `fpcalc -raw` na prvih ~10 min i zadnjih ~10 min svake epizode → usporedba fingerprinta parova epizoda iste sezone → najduži zajednički segment = Intro / Outro.
- Fallback za credits: `blackdetect` + `silencedetect` u zadnjih 15 %.
- Izvor i confidence spremljeni; ručna korekcija uvijek pobjeđuje; segment iz imena poglavlja kao treći izvor.

### 5.5 Auth
- Lozinke argon2id. Sesijski token = 32 B random, u DB-u samo SHA-256 hash; vezan uz device. Header `Authorization: Streamero token="…"` + `Bearer` za API ključeve.
- Stream URL-ovi: `?sig=<HMAC(item, session, exp)>&exp=` (AVPlayer, Cast, `<video>` ne mogu slati headere).
- Quick Connect: `POST /quickconnect/initiate` → kod + secret; prijavljeni uređaj `POST /quickconnect/authorize {code}`; novi uređaj pollira/WS.

### 5.6 Extensibility (umjesto .NET plugina)
Jellyfin plugini su in-process .NET DLL-ovi. Mi:
1. Većinu popularnih plugina **ugrađujemo** (vidi `[JF-P]` u FEATURES.md).
2. **Webhooks + event bus** za integracije.
3. **External providers preko HTTP-a**: admin registrira URL sidecar servisa koji implementira mali OpenAPI ugovor (`/metadata/search`, `/metadata/item`, `/segments`, `/search`) — bilo koji jezik, bilo koji kontejner.
4. Web UI ekstenzije: admin može dodati custom CSS/JS (kao JS Injector) — iza upozorenja.

---

## 6. iOS app i Feather

### Build bez potpisivanja (GitHub Actions, `macos-15`)
```bash
brew install xcodegen
cd ios && xcodegen generate
xcodebuild -project Streamero.xcodeproj -scheme Streamero -configuration Release \
  -sdk iphoneos -destination 'generic/platform=iOS' \
  -derivedDataPath build \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY="" \
  build
mkdir -p Payload && cp -R build/Build/Products/Release-iphoneos/Streamero.app Payload/
zip -qry Streamero-${VERSION}.ipa Payload
```
Artefakt `.ipa` → GitHub Release. Feather ga potpiše tvojim certifikatom.

### Feather source (auto-update)
`feather/source.json` u AltStore source formatu (name, identifier, apps[{bundleIdentifier, versions[{version, buildVersion, date, downloadURL, size, localizationDescription}], iconURL, …}]). `release.yml` nakon uploada `.ipa` dopisuje novu verziju i commita. U Featheru dodaš source URL `https://raw.githubusercontent.com/<owner>/streamero/main/feather/source.json` (ili ga servira sam Streamero server na `/ios/source.json` — pa radi i ako je repo privatan).

### Identitet
- Display name: **Streamero**; bundle ID prijedlog: `net.scyde.streamero` (potvrdi); developer: SCYDE.
- Min iOS 17. iPhone + iPad.

### Ograničenja sideloada (važno za dizajn)
| Feature | Status kod Feather sideloada | Odluka |
|---|---|---|
| Background audio, PiP, AirPlay, background downloadi | radi (Info.plist background modes) | ✅ |
| Bonjour discovery (`NWBrowser`, `NSBonjourServices: _streamero._tcp`) | radi, ne treba multicast entitlement | ✅ koristimo mDNS, NE UDP broadcast (broadcast traži restricted entitlement) |
| Google Cast SDK | radi (local network permission + `_googlecast._tcp`) | ✅ Default Media Receiver ili vlastiti receiver |
| Push notifikacije (APNs) | nepouzdano (ovisi o certifikatu/entitlementu) | ❌ umjesto toga WebSocket + lokalne notifikacije; vanjski kanal ntfy/Telegram |
| CarPlay audio | traži entitlement koji ovisi o certifikatu | ⚠️ iza feature flaga, ne blokira ništa |
| Widgeti / App Group | app group ID se kod resigna mijenja | ⚠️ zadnja faza, opcionalno |
| Keychain, Face ID | radi | ✅ |

### Player strategija
- **Primarno AVPlayer + HLS** od servera (remux u fMP4 kad je moguće bez transkoda) → najbolja kvaliteta HDR/DV, PiP, AirPlay, baterija.
- **MPVKit** kao druga engine opcija (postavka + automatski kad server javi da bi AVPlayer zahtijevao transcode, a MPV može direct play — npr. ASS styling, PGS, DTS, VC-1).
- Zajednički `PlayerEngine` protokol, isti UI overlay za oba.

---

## 7. CI/CD i rad na vlastitom serveru

- **Dev na serveru** (Claude Code u remote sessionu, isto kao Mediarr): `docker compose -f deploy/docker-compose.yml -f deploy/compose.dev.yml up --build` — `compose.dev.yml` builda lokalno (`build: ..`) umjesto GHCR imagea i montira test medije iz `scripts/gen-fixtures.sh`.
- **Branch strategija:** `main` uvijek deployabilan; po fazi branch `phase/P5-playback`; PR → CI zelen → merge.
- `ci.yml`: `go vet`, `golangci-lint`, `go test ./... -race`, `sqlc diff`, provjera da je generirani API kod svjež, `pnpm lint/typecheck/test`, Playwright smoke protiv compose stacka.
- `server-image.yml`: na push u main → `ghcr.io/<owner>/streamero:edge`; na tag `v*` → `:X.Y.Z` + `:latest`.
- `ios.yml`: na promjene u `ios/**` ili `api/**` → unsigned `.ipa` artefakt (+ unit testovi na simulatoru).
- `release.yml`: tag → GitHub Release s `.ipa`, changelog, update `feather/source.json`.
- Deploy na produkcijski server: `docker compose pull && docker compose up -d` (ili Watchtower na `:latest`).

---

## 8. Faze

Svaka faza = jedan branch, jedan ili više PR-ova. **Kriteriji prihvaćanja moraju proći prije prelaska na sljedeću fazu.** Nakon svake faze: ažuriraj `docs/PROGRESS.md`, označi stavke u `FEATURES.md`, zapiši odluke u `docs/DECISIONS.md`.

### P0 — Temelji (repo, compose, CI, API pipeline, prazne ljuske)
- Monorepo struktura iz §2, `CLAUDE.md`, `.env.example`, Dockerfile, compose (+dev override), Postgres + Valkey + River.
- `streamero serve|worker|migrate|healthcheck`, `/healthz`, `/readyz`, `GET /api/v1/system/info`, 503 + Retry-After dok se pokreće.
- OpenAPI pipeline: `api/openapi.yaml` → ogen (Go), openapi-typescript + TanStack Query hookovi (web), swift-openapi-generator (iOS).
- Web: shell s routingom, i18n (HR/EN), tema, prazan login ekran, embed u Go binarij.
- iOS: XcodeGen projekt, ekran „Dodaj server" (URL + provjera `/system/info`), CI daje `.ipa`.
- `scripts/gen-fixtures.sh` generira test medije.
- **Prihvaćanje:** `docker compose up` na serveru → web otvara login na `:8787`; GitHub Actions daje `.ipa` koja se instalira kroz Feather i spoji na server; CI zelen.

### P1 — Korisnici i auth
Setup wizard, users, profili s PIN-om, dozvole, parental postavke (model + API), sesije, uređaji (opoziv gasi sesije), API ključevi, Quick Connect, TOTP 2FA, invite linkovi, lockout, rate limit, audit log osnova.
- **Prihvaćanje:** wizard → admin → pozovi drugog korisnika linkom → login na webu i iOS-u (lozinka + Quick Connect) → opoziv uređaja odmah odjavljuje.

### P2 — Biblioteke i scanner
Tipovi biblioteka (filmovi, serije, mixed za početak), putanje, naming parseri + golden testovi (min. 300 slučajeva), ffprobe → media_sources/streams, verzije, extras, ignore pravila, provider ID u imenima, inkrementalni scan, fsnotify, rename-preserve, zamjena datoteke čisti izvedene podatke.
- **Prihvaćanje:** fixture biblioteka skenirana ispravno (golden snapshot); drugi scan bez promjena < 2 s za fixtures i < 30 s za 50k sintetičkih stavki.

### P3 — Metapodaci i slike
Provider orchestrator (redoslijed po biblioteci, jezik/država), TMDb, TVDB, OMDb, Fanart, AniList, NFO, embedded; slike (cache, libvips resize, AVIF/WebP, blurhash); ljudi; dobne ocjene po državama; automatske kolekcije; missing/unaired epizode; Identify; metadata editor API + lock polja; rate limiting prema providerima.
- **Prihvaćanje:** 20 stvarnih filmova i 5 serija dobiju točne metapodatke i slike na HR i EN; Identify ispravlja krivi match; zaključano polje preživi refresh.

### P4 — Web: pregledavanje
Home (Continue Watching, Next Up, Latest, media bar, prilagodba redova), biblioteke (grid/list/folder), svi filteri/sortovi iz FEATURES §5, A–Z, cursor paginacija, detalji (sve sekcije), akcije, pretraga, favoriti, ocjene, kolekcije, playliste, skrivanje, parental filtriranje svugdje, teme + custom CSS, screensaver, theme songs, gamepad/tipkovnica.
- **Prihvaćanje:** Lighthouse performance ≥ 90; home < 200 ms TTFB API-ja na 50k biblioteci; Playwright testovi glavnih tokova.

### P4b — *arr ekosustav (D-011)
Compat API podskup pod `/compat` (po javnoj OpenAPI specifikaciji) za Seerr/Jellyseerr: login, uvoz korisnika, biblioteke, items s TMDB/TVDB/IMDb ID-evima, sezone/epizode, nedavno dodano, refresh. Ulazni webhooks Radarr/Sonarr/Lidarr (generic Webhook + Emby/Jellyfin connect) → ciljani scan. Bazarr: sidecar podnaslovi preko fsnotify + post-processing endpoint. `docs/INTEGRATIONS.md` s uputama korak-po-korak za svaki alat.
- **Prihvaćanje:** u CI-ju (compose profil `arr`, stvarni kontejneri): Seerr i Jellyseerr se spoje na Streamero, uvezu korisnika i označe fixture film/seriju kao *Available*; Radarr/Sonarr/Lidarr import pokrene scan samo te putanje i stavka se pojavi < 10 s; Bazarr `.srt` se pojavi u stavci bez ručnog scana. Isto ručno provjereno na demo.streamero.net.

### P5 — Reprodukcija (server + web player)
Device profili, odluka o reprodukciji (čista funkcija + testovi), direct play (range), HLS remux, HLS transcode, HW accel (VA-API i NVENC obavezno testirani; QSV/AMF/RKMPP/VT kod puta), tonemapping, podnaslovi (sve iz FEATURES §6), audio odabir/normalizacija, potpisani URL-ovi, progress reporting, resume, mark played prag, Next Up autoplay, „Još gledate?", sleep timer, frame-step, info overlay, PiP, queue, cinema mode, transcode cleanup + limiti, razlog transkodiranja.
- **Prihvaćanje:** matrica fixture × klijent (Chrome, Firefox, Safari) reproducira bez greške; seek u transkodiranom 4K HDR → SDR radi < 3 s; nema zaostalih ffmpeg procesa nakon zatvaranja playera.

### P6 — Trickplay, poglavlja, segmenti
Trickplay task i serviranje, chapter images, media segments API, provider iz poglavlja, chromaprint intro/outro detekcija, blackdetect fallback, editor segmenata u webu, skip gumbi + binge mode + skip-credits→next, EDL export.
- **Prihvaćanje:** na fixture „sezoni" od 4 epizode sa zajedničkim introm detekcija pogodi ±1 s; skip radi na webu.

### P7 — iOS core
Server/korisnik/profil odabir, Bonjour, login (lozinka + Quick Connect), Home, biblioteke, detalji, pretraga, favoriti, AVPlayer engine (HLS, HDR, PiP, AirPlay, Now Playing), podnaslovi/audio/brzina/poglavlja/trickplay scrubber, skip intro/credits, Next Up, „Još gledate?", HR/EN.
- **Prihvaćanje:** na stvarnom iPhoneu preko Feathera: od instalacije do gledanja epizode s intro skipom bez ijednog crasha; resume se sinkronizira s webom.

### P8 — iOS napredno
MPVKit engine, downloadi (background, kvaliteta, sezone, auto-next-N), offline mod + sync napretka, optimized versions na serveru, Chromecast, Face ID lock, iPad layout.
- **Prihvaćanje:** u airplane modu pogledaš preuzetu epizodu; kad se vratiš online, napredak je na serveru.

### P9 — Glazba, knjige, audioknjige, fotografije, music videi
Server (parseri, MusicBrainz/AudioDB, ReplayGain, lyrics, OPF/ComicInfo, page count, posteri), web (layouti, glazbeni player s queueom i tekstovima, čitač EPUB/PDF/comics, slideshow), iOS (glazbeni player background + lock screen, gapless, čitač, audioknjige; CarPlay iza flaga).
- **Prihvaćanje:** album s FLAC-om svira gapless na iOS-u u pozadini; CBZ i EPUB čitljivi i pamte stranicu između weba i iOS-a.

### P10 — Admin dashboard
Sesije (stop, poruka, razlog transkoda), storage/brojevi, activity + audit log (sort/filter), scheduled tasks UI (triggeri, napredak live), log viewer live, sve postavke iz FEATURES §13, networking, branding, backup/restore, Metadata Manager, startup UI, transcode pravila (killer), GeoIP.
- **Prihvaćanje:** admin može sve iz FEATURES §13 bez dodira `.env`-a ili baze.

### P11 — Live TV i DVR
HDHomeRun + M3U, XMLTV + Schedules Direct, mapiranje, guide (web + iOS), live playback (remux/transcode), timeshift, snimanje (pojedinačno + serije, padding, konflikti), snimke kao biblioteka, comskip segmenti.
- **Prihvaćanje:** s javnim test M3U + XMLTV: guide se prikazuje, kanal se gleda, zakazana snimka nastane i ima Commercial segmente.

### P12 — Watch Party i remote control
SyncPlay protokol preko WS-a (grupe, ready/buffering stanja, drift korekcija, zajednički queue), chat + reakcije + invite link, remote control sesija (web ↔ iOS).
- **Prihvaćanje:** web + iPhone gledaju zajedno, razlika < 300 ms nakon seeka; pauza na jednom pauzira oba.

### P13 — Extras (ono što Jellyfin nema ili ima samo kroz plugine)
OIDC SSO, LDAP, passkeys; OpenSubtitles + auto-sync + Whisper + LLM prijevod; Requests (izlazno prema Radarr/Sonarr/Lidarr; ulazni webhooks su u P4b); webhooks/notifikacije/newsletter; Trakt/AniList/Last.fm/ListenBrainz; statistike + Wrapped + Prometheus/Grafana; pametne kolekcije; AI semantička pretraga (pgvector + Ollama); media health; media cleaner; OPDS; External providers ugovor; Jellyfin import.
- **Prihvaćanje:** svaka stavka ima vlastiti e2e ili integracijski test; Jellyfin import prenese watch status 1:1 na testnom Jellyfin 12 serveru.

### P14 — Hardening i release
Security pregled (path traversal fuzz, auth testovi, CSP, dependency audit), perf testovi (50k i 200k stavki, 10 istovremenih transkoda), load test WS-a, backup/restore drill, dokumentacija (`DEPLOY.md`, `IOS.md`, `NAMING.md`), proširenje compat sloja iz P4b za Infuse/Swiftfin/Findroid, DLNA, ABR, widget; open-source priprema (licenca, CONTRIBUTING).
- **Prihvaćanje:** `v1.0.0` tag → image + `.ipa` + Feather source automatski; čisti install po `DEPLOY.md` uspijeva iz nule za < 10 min.

---

## 9. Promptovi za Claude Code (copy-paste po fazi)

Opći uvod (za svaku sesiju):
```
Radiš na projektu Streamero. Pročitaj CLAUDE.md, docs/PLAN.md, docs/FEATURES.md i docs/PROGRESS.md.
Danas radimo fazu <PX>. Napravi branch phase/<PX>-<ime>. Prvo mi napiši kratak plan koraka
za ovu fazu (datoteke, migracije, endpointi, testovi), zatim implementiraj. Poštuj kriterije
prihvaćanja iz PLAN.md. Na kraju: pokreni sve testove, ažuriraj PROGRESS.md i FEATURES.md,
otvori PR s opisom.
```

P0:
```
Faza P0. Postavi monorepo točno po PLAN.md §2. Server u Go (chi + ogen, sqlc, goose, River),
Postgres+pgvector, Valkey, Dockerfile multi-stage s jellyfin-ffmpeg, deploy/docker-compose.yml
po §3 + compose.dev.yml koji builda lokalno. Web: React+Vite+TS+Tailwind+TanStack, i18n HR/EN,
embed u Go binarij. iOS: XcodeGen project.yml, SwiftUI app "Streamero" s ekranom za dodavanje
servera. GitHub Actions: ci.yml, server-image.yml, ios.yml (unsigned .ipa po PLAN.md §6),
release.yml + feather/source.json. scripts/gen-fixtures.sh. Provjeri da `docker compose up`
radi na ovom serveru.
```

P2 (primjer specifičnog):
```
Faza P2. Najprije internal/naming s golden testovima u server/testdata/naming (min. 300 stvarnih
imena: filmovi s godinom i bez, verzije, extras, serije SxxEyy, multi-episode, daily, anime
apsolutno, specials, provider ID u [], {}, ()). Pravila dokumentiraj u docs/NAMING.md.
Tek onda scanner: inkrementalni po (path,size,mtime,inode), fsnotify s debounceom, River jobovi
za probe. Dodaj scripts/seed-big-library.sh i perf test iz kriterija prihvaćanja.
```

P5:
```
Faza P5. Odluka o reprodukciji kao čista funkcija internal/playback.Decide(profile, source, limits)
s tabličnim testovima za profile: ios-avplayer, ios-mpv, safari, chrome, firefox, chromecast.
Zatim HLS (fMP4/TS), seek-restart, throttling, cleanup, HW accel builder za vaapi/nvenc/qsv/amf/
videotoolbox/rkmpp s tonemapom. Web player na hls.js + JASSUB za ASS. Napravi matricu testova
fixture × browser u Playwrightu.
```

P7:
```
Faza P7. iOS core prema PLAN.md §6. Koristi generirani Swift klijent iz api/openapi.yaml.
PlayerEngine protokol + AVPlayerEngine. Bonjour preko NWBrowser (_streamero._tcp), NE UDP broadcast.
Sve stringove u Localizable.xcstrings (HR+EN). Ne koristi APNs. Provjeri da ios.yml producira .ipa.
```

Za ostale faze koristi opći uvod + naziv faze; detalji su u §8 i FEATURES.md.

---

## 10. Otvorena pitanja (odluči prije ili tijekom P0)
1. Bundle ID (`net.scyde.streamero`?) i GitHub owner/repo (`byscyde/streamero`?).
2. Dizajn referenca za web i iOS (link, zip ili screenshotovi) — dodaj u `docs/DESIGN.md`.
3. Hoće li produkcijski server imati GPU (VA-API AMD/Intel ili NVIDIA) — određuje koji HW put se testira prvi.
4. Domena i reverse proxy (Caddy / NPM / Traefik) za remote pristup.
5. Treba li Android kasnije (utječe samo na to da API ostane klijent-neutralan — već je).
