# syntax=docker/dockerfile:1.7
# Multi-stage: web build -> Go build (web embedded) -> runtime with ffmpeg.
# Build context is the repository root: docker build -f deploy/Dockerfile .

FROM node:22-bookworm-slim AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/.npmrc ./
RUN pnpm install --frozen-lockfile
COPY api /src/api
COPY web ./
RUN mkdir -p /src/server/internal/webui && pnpm build

FROM golang:1.26-trixie AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
COPY --from=web /src/server/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/streamero-app/streamero/server/internal/version.Version=${VERSION} -X github.com/streamero-app/streamero/server/internal/version.Commit=${COMMIT}" \
    -o /out/streamero ./cmd/streamero

FROM debian:trixie-slim AS runtime
ARG TARGETARCH
# FFMPEG_SOURCE=extended installs a third-party ffmpeg 8 build with VA-API/QSV/NVENC/Vulkan/OpenCL
# support (only run as an external process, see THIRD_PARTY.md). FFMPEG_SOURCE=debian uses Debian's
# ffmpeg. The old value "jellyfin" is still accepted for existing build scripts.
ARG FFMPEG_SOURCE=extended
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg tzdata libvips42t64 libchromaprint-tools mesa-va-drivers; \
    if [ "$TARGETARCH" = "amd64" ]; then \
      sed -i 's/^Components: main$/Components: main non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources; \
      apt-get update; apt-get install -y --no-install-recommends intel-media-va-driver-non-free; \
    fi; \
    if [ "$FFMPEG_SOURCE" = "extended" ] || [ "$FFMPEG_SOURCE" = "jellyfin" ]; then \
      curl -fsSL https://repo.jellyfin.org/jellyfin_team.gpg.key | gpg --dearmor -o /usr/share/keyrings/jellyfin.gpg; \
      echo "deb [signed-by=/usr/share/keyrings/jellyfin.gpg] https://repo.jellyfin.org/debian trixie main" > /etc/apt/sources.list.d/jellyfin.list; \
      apt-get update; apt-get install -y --no-install-recommends jellyfin-ffmpeg8; \
      ln -s /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/local/bin/ffmpeg; \
      ln -s /usr/lib/jellyfin-ffmpeg/ffprobe /usr/local/bin/ffprobe; \
    else \
      apt-get install -y --no-install-recommends ffmpeg; \
    fi; \
    apt-get purge -y gnupg; apt-get autoremove -y; rm -rf /var/lib/apt/lists/*; \
    useradd --uid 1000 --create-home --shell /usr/sbin/nologin streamero; \
    mkdir -p /config /cache /transcode /media && chown streamero:streamero /config /cache /transcode
COPY --from=server /out/streamero /app/streamero
USER streamero
EXPOSE 8787
ENV STREAMERO_HTTP_ADDR=:8787
HEALTHCHECK --interval=15s --timeout=5s --retries=5 CMD ["/app/streamero", "healthcheck"]
ENTRYPOINT ["/app/streamero"]
CMD ["serve"]
