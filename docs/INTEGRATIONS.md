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
Streamero through the same compat layer (D-031). The server address is your
Streamero URL **with `/compat`**, e.g. `https://media.example.com/compat` or
`http://192.168.1.10:8787/compat`. Choose "Jellyfin" as the server type where
the app asks. Server discovery by UDP broadcast is not offered: type the address.

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
intro/credits segments from media analysis.

| Client | Status |
|---|---|
| Jellyfin for **Android TV** 0.19.10 | **Verified** in an Android TV 12 emulator against a Streamero dev server: add server by address, Quick Connect for a two-factor account (approved from the web), home rows, library grid, series page with Next up, favourite, direct play (H.264/AAC MKV), automatic fallback to HLS transcode when the device cannot decode the file (H.264 4:4:4), progress and resume position saved. |
| Infuse (iPhone / Apple TV) | Built from the public API description only — not tried. |
| Swiftfin (iOS / tvOS) | Built from the public API description only — not tried. |
| Kodi add-on | Built from the public API description only — not tried. "Native" (path) mode cannot work: Streamero does not expose file paths. |
| Samsung (Tizen) / LG (webOS) apps | Not tried; they use the same API as the Android TV app, so similar behaviour is expected but unconfirmed. |

How the Android TV check was done: the official release APK was installed in an
emulator, driven with remote-control key events, and the server's request log
(`STREAMERO_LOG_LEVEL=debug` prints every compat call — method, path, status and
parameter names, never values) was compared with the integration test
`TestCompatClientSessionIntegration`. No client source code was read.

Known gaps: no WebSocket (`/socket`), so no remote control, no live "now playing"
push and no SyncPlay; display preferences are not stored; music is direct play
only; files scanned before 00024 lack the video profile until they change on
disk (direct play is then attempted and, if it fails, the stream is transcoded);
live TV, collections/playlists and parental ratings are not exposed.

Diagnosing a client: start the server with `STREAMERO_LOG_LEVEL=debug` and look
for `compat request` lines with status 404 (an endpoint the client needs that
Streamero does not offer) or 401.

## Others
Tools that support "Jellyfin" as a media server may work against `/compat` with an
API key; only the endpoints listed in `server/internal/compat` exist — anything else
answers 404 instead of guessing.
