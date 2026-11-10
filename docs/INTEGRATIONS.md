# Integrations

Streamero offers a compatibility API under `/compat` that speaks the part of the
Jellyfin-style HTTP API used by request managers, the *arr apps (D-011, D-016) and
media clients (D-031, see below). Integrations use an admin account or an API key
(Settings → API keys).

## Seerr / Jellyseerr
Verified with Jellyseerr 2.7.3: sign-in, user import, library sync, availability
(movies; series per season, partially available), 4K detection from the files.

Setup → *Jellyfin*:
| Field | Value |
|---|---|
| Hostname / IP | your Streamero host (e.g. `streamero` in the same Docker network, or `demo.example.com`) |
| Port | `8787` (or `443` behind HTTPS) |
| Use SSL | on behind HTTPS |
| URL Base | `/compat` |
| Username / Password | a Streamero admin without two-factor sign-in |

Then *Sync libraries*, enable them and run *Jellyfin Full Scan*. Items are matched
by TMDb/IMDb/TVDb IDs, so set a TMDb key in Settings → Metadata (or put
`[tmdbid-…]` in folder names). "Play on" links open the item in Streamero.

## Radarr / Sonarr / Lidarr
Settings → Connect → **+ → Emby / Jellyfin**:
| Field | Value |
|---|---|
| Host | Streamero host |
| Port | `8787` |
| URL Base | `/compat` |
| API Key | a Streamero API key (Settings → API keys) |
| Send Notifications | **off** (Streamero has no pop-up notifications) |
| Update Library | on |

After an import, rename or delete, Streamero scans the library that contains the
reported path (all libraries if the path is from a different mount point). The
file watcher also picks up changes on local disks within ~15 s.

## Bazarr
Nothing to configure: Bazarr saves subtitles next to the video
(`Movie (2021).hr.srt`); Streamero's file watcher adds them within ~15 s.

## Prowlarr
Prowlarr only talks to the *arr apps (indexers); it has no media-server connection.

## Connecting Infuse / Swiftfin / Kodi / TV apps
Media clients that speak the Jellyfin client API can browse and play from
Streamero through the same compat layer (D-031). The server address is simply
your Streamero URL, e.g. `https://media.example.com` or `http://192.168.1.10:8787`:
the Jellyfin-style paths these apps call (`/System/...`, `/Users/...`, `/Items/...`,
`/socket`, …) are answered at the root (D-032). The old form with `/compat`
(`http://192.168.1.10:8787/compat`) keeps working; set `STREAMERO_COMPAT_ROOT=false`
to serve the API only there. Choose "Jellyfin" as the server type where the app asks.

**Finding the server automatically.** Apps that offer "servers on this network"
send a broadcast to UDP port 7359; Streamero answers with its address, id and
name (D-033, on by default, `STREAMERO_DISCOVERY=false` turns it off; only
senders on private, loopback or link-local addresses get an answer). With Docker:
- the compose file publishes `7359/udp`, but a **bridge network usually does not
  pass LAN broadcasts into the container**. For discovery use host networking
  (`network_mode: host` on the `streamero` service), or just type the address;
- the container only knows its internal address, so set
  `STREAMERO_DISCOVERY_URL=http://<LAN address of the host>:8787` in `deploy/.env`,
  otherwise the app is told an address it cannot reach.

Sign in with a Streamero user name and password. Accounts with **two-factor
sign-in** cannot use the password there; use **Quick Connect** instead: the app
shows a six-digit code, approve it in Streamero under *Settings → Quick Connect*
(or from another app that is already signed in). Users need access to all
libraries to see anything (per-library access is not mapped yet).

What works: libraries, browsing with paging/sorting/filters, series → seasons →
episodes, posters/backdrops/logos/person photos, search, Continue watching, Next
up, Latest, watched/favourite/position (read and write), playback info with
direct play when the app can decode the file and Streamero's HLS transcode
otherwise, subtitles (sidecar and embedded text tracks; bitmap tracks only in
direct play), progress reports (resume position, "now playing" on the dashboard),
intro/credits segments from media analysis, and a WebSocket (`/socket`) over
which the admin dashboard's **message** and **stop** commands reach the app.

### TV apps

| TV | How to connect | Status |
|---|---|---|
| **Android TV / Google TV** — official app 0.19.10 | Install "Jellyfin" from Google Play, *Add server*, pick the discovered server or type `http://<server>:8787`, sign in (or Quick Connect). | **Verified** in an Android TV 12 emulator (with `/compat`, previous release): add server, Quick Connect for a two-factor account, home rows, library grid, series page with Next up, favourite, direct play (H.264/AAC MKV), automatic fallback to HLS transcode, progress and resume saved. Root address, discovery and `/socket` are covered by integration tests only, not yet re-checked in the app. |
| **Samsung (Tizen)** — official app (Samsung store, 2021+ TVs) | Same as above: type `http://<server>:8787` or pick the discovered server. | **Not tried.** The app is a web app with the web client packaged inside it, calling the server API from its own origin; Streamero therefore answers cross-origin requests on the compat API (CORS, any origin, no cookies). Expected to behave like the Android TV app, but it uses more of the API than that app and some calls may be missing — check the debug log (below). |
| **LG (webOS)** — official app (LG Content Store) | — | **Does not work.** The app is a thin wrapper that loads the web client *from the server* (`/web/index.html`, `/web/manifest.json` and its scripts) and checks that it is there. Streamero does not ship that web client (clean-room, D-031) and its own web app is not built for a TV remote. Use an Android TV / Google TV stick or Infuse on Apple TV meanwhile. See D-034. |

How the Android TV check was done: the official release APK was installed in an
emulator, driven with remote-control key events, and the server's request log
(`STREAMERO_LOG_LEVEL=debug` prints every compat call — method, path, status and
parameter names, never values) was compared with the integration test
`TestCompatClientSessionIntegration`. How the Tizen and webOS apps work was taken
from public sources only (Jellyfin forum answers by the maintainers, users'
access logs, store and download pages, LG/Samsung developer documentation); no
client source code or app package was opened. Neither TV's emulator was run:
the webOS emulator needs VirtualBox (and LG's newer simulator is a desktop app),
the Tizen TV emulator needs Tizen Studio plus a GUI, and the official Tizen app is
only distributed through the TV's store.

### Other clients

| Client | Status |
|---|---|
| Infuse (iPhone / Apple TV) | Built from the public API description only — not tried. |
| Swiftfin (iOS / tvOS) | Built from the public API description only — not tried. |
| Kodi add-on | Built from the public API description only — not tried. "Native" (path) mode cannot work: Streamero does not expose file paths. |

Known gaps: `/socket` carries only keepalive and the dashboard's message/stop —
no remote control from other apps, no live library/user-data events, no SyncPlay;
display preferences are not stored; music is direct play only; files scanned
before 00024 lack the video profile until they change on disk (direct play is
then attempted and, if it fails, the stream is transcoded); live TV,
collections/playlists and parental ratings are not exposed.

Diagnosing a client: start the server with `STREAMERO_LOG_LEVEL=debug` and look
for `compat request` lines with status 404 (an endpoint the client needs that
Streamero does not offer) or 401.

## Others
Tools that support "Jellyfin" as a media server may work against `/compat` with an
API key; only the endpoints listed in `server/internal/compat` exist — anything else
answers 404 instead of guessing.
