# Live TV and DVR

Streamero can show live TV from IPTV playlists (M3U) and HDHomeRun network tuners,
with an XMLTV programme guide, time-shift (pause and rewind live TV) and a DVR that
records single programmes and whole series. Design notes: D-041 in `DECISIONS.md`.

Everything is set up in the web client under **Settings → Live TV** by administrators
and by users with the **Manage Live TV** permission. Watching needs the **Live TV**
permission (on by default for new users).

## 1. Tuners

### M3U playlist
Give the playlist's **URL** (`http://` or `https://`) or a **file** on the server. Files
must be inside the media folders or in `/config/livetv` (the `livetv` folder of
`CONFIG_PATH`); any other path is refused.

- Extended M3U attributes are read: `tvg-id`, `tvg-name`, `tvg-logo`, `group-title`,
  `tvg-chno` / `channel-number`, `#EXTGRP`, and `#EXTVLCOPT:http-user-agent`.
- The playlist may be UTF-8 (with or without BOM), UTF-16 with BOM, or Windows-1252.
- Channel addresses must be network streams (`http(s)`, `udp`, `rtp`, `rtsp(s)`,
  `rtmp(s)`); local files and other ffmpeg protocols in a playlist are skipped.
- **Tuner count** is how many streams the provider allows at once (0 = unlimited).
- **User agent** is sent to the provider (some require a specific one).

### HDHomeRun
Use **Find on network** (SSDP search, confirmed through the device's
`/discover.json`) or type the device address (e.g. `192.168.1.50`). With Docker's
default bridge network the search finds nothing; type the address instead. The tuner
count comes from the device; it can be lowered. Channels protected by DRM are left out.

**Test** checks a configuration without saving it.

## 2. Guide (XMLTV)
Add the guide's URL or file (same folder rules as playlists; `.xml` or gzip-compressed).
Guides in legacy encodings (ISO-8859-x, Windows-125x…) are read through their XML
declaration. **Time zone** applies to guide times written without an offset (default
UTC). Guides reload every **Refresh hours** (the hourly task *Refresh guide* checks
which are due); an unchanged guide (HTTP 304, same content) is skipped, and only new or
changed programmes are written.

### Channel mapping
Channels are mapped to guide channels automatically: by `tvg-id`, then by name
(accents, case, punctuation and suffixes such as "HD" ignored), then — for HDHomeRun —
by channel number. **Settings → Live TV → Channels** shows the result; a channel can be
mapped by hand, unmapped, hidden, or given a category (movies, sports, news, kids).

## 3. Watching
Live TV is always delivered as HLS. A channel is **remuxed** (stream copy) when the
device plays its codecs, otherwise **transcoded** with the server's encoding settings
(hardware acceleration included); interlaced broadcasts are deinterlaced when
transcoded. The transcoding permissions of the account apply as for films.

**Time-shift:** the stream is a rolling window of **Time-shift buffer** minutes (default
30). Viewers can pause and rewind inside it; older segments are deleted, so the buffer
never grows. Viewers of the same channel with the same delivery share one stream and
one tuner. A recording that needs a tuner stops a live viewer if no tuner is free.

## 4. Recording
Recordings are written to `/recordings` (`RECORDINGS_PATH` on the host,
`STREAMERO_RECORDINGS_DIR` inside the container) as MPEG-TS files, laid out the way the
library reads them:

```
<Title>/Season N/<Title> - SxxEyy - <Episode>.ts     season and episode known
Movies/<Title (Year)>/<Title (Year)>.ts              films
<Title>/<Title> - YYYY-MM-DD HH.MM - <Episode>.ts    everything else (by air date)
```

A Kodi `.nfo` with the guide's title, description, categories, air date and channel is
written beside each recording, and the **Recordings** library (created with the first
recording) picks them up.

- **Padding:** start early / end late (defaults in the DVR settings, per recording and
  per series rule).
- **Series rules** record every airing of a title, or only new episodes (airings the
  guide marks as repeats are skipped, and each episode is recorded once), on any channel
  or one channel.
- **Priorities and conflicts:** when a source runs out of tuners, the recording with the
  higher priority (then the one scheduled first) wins; the others are shown as conflicts
  and are recorded if a tuner frees up.
- If the server restarts during a recording, it continues into the same file.

### Post-processing command
Administrators can set a program that runs after every finished recording (for example
to detect commercials or convert the file). It runs **without a shell**: the command is
an absolute path to an executable, and each argument is passed as exactly one argument
after these placeholders are replaced:

| Placeholder | Value |
|---|---|
| `{path}` | full path of the recording |
| `{dir}` | its folder |
| `{filename}` | its file name |
| `{title}` / `{subtitle}` | programme and episode title |
| `{channel}` / `{channelNumber}` | channel name and number |
| `{start}` / `{end}` | programme start and end (RFC 3339, UTC) |
| `{season}` / `{episode}` | numbers, empty when unknown |
| `{recordingId}` | the recording's id |

The program runs in the recording's folder with a minimal environment (`PATH`,
`LANG`, `HOME`, `STREAMERO_RECORDING_PATH`, `STREAMERO_RECORDING_ID`) and is stopped
after the timeout. Its exit status and last output are shown on the recording when it
fails. Leave the recording at its path: the library finds it there.

Example (Comskip writing an EDL beside the file): command `/usr/local/bin/comskip`,
arguments `--quiet`, `--output={dir}`, `{path}`.

### Retention (opt-in)
Off by default. When turned on, finished recordings are deleted when older than
**Delete after (days)**, or beyond the newest **Keep last** recordings of a series rule
(a rule can override the number). Only files the DVR recorded itself — `.ts` files at
the path it stored, inside the recordings folder — and the `.nfo` it wrote are ever
deleted; nothing else in the recordings folder or the media folders is touched.
Removing a recording from the list in the apps never deletes its file.

## 5. Apps and other clients
- Web: Live TV (guide, channels, recordings, schedule, series rules), "On now" on Home.
- iOS and Android: channel list with now/next, playing a channel, recording a programme
  or series from the channel's programme list. No guide grid in the apps.
- Media clients using the compatibility API (TV apps): channels, guide and channel
  playback (`/LiveTv/Channels`, `/LiveTv/Programs`, `/LiveTv/Programs/Recommended`,
  `/LiveTv/GuideInfo`, `/LiveTv/Info`, `PlaybackInfo` of a channel). Recordings, timers
  and series timers are listed empty there: manage them in Streamero's apps.
