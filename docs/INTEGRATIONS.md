# Integrations

Streamero offers a compatibility API under `/compat` that speaks the part of the
Jellyfin-style HTTP API used by request managers and the *arr apps (D-011, D-016).
Use an admin account or an API key (Settings → API keys).

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

## Others
Tools that support "Jellyfin" as a media server may work against `/compat` with an
API key; only the endpoints listed in `server/internal/compat` exist — anything else
answers 404 instead of guessing.
