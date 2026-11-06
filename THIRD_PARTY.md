# Third-party software

Streamero invokes or links the following components. Licenses apply to those components only.

| Component | Use | License |
|---|---|---|
| FFmpeg (`jellyfin-ffmpeg8` build or Debian `ffmpeg`) | Executed as an external process for probing/transcoding; not linked | GPL-3.0 / LGPL-2.1 (build dependent); source: https://github.com/jellyfin/jellyfin-ffmpeg |
| libvips | Image processing (runtime library) | LGPL-2.1 |
| Chromaprint (`fpcalc`) | Audio fingerprinting, external process | LGPL-2.1 / MIT |
| PostgreSQL + pgvector | Database | PostgreSQL License |
| Valkey | Cache / pub-sub | BSD-3-Clause |
| Go modules (chi, pgx, goose, River, ogen, go-redis, uuid) | Server | MIT / Apache-2.0 / BSD |
| React, TanStack, i18next, Tailwind CSS, Vite | Web client | MIT |
| swift-openapi-runtime / urlsession / generator | iOS client | Apache-2.0 |

Full dependency license lists are generated in CI before release (P14).
