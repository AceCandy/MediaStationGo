# syntax=docker/dockerfile:1.6
# =============================================================================
# Multi-architecture build for MediaStationGo.
#
# Stage 1 (frontend) :  Node 22  -> static SPA bundle
# Stage 2 (backend)  :  Go 1.25  -> dynamic binary for native WebP
# Stage 3 (runtime)  :  Alpine 3.23 -> ffprobe + tzdata + non-root user
#
# Build:
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     --build-arg VERSION=MediaStationGo-v0.1.16 -t mediastation-go:latest --push .
#
# =============================================================================

# ---- Stage 1: frontend (always build on the host architecture) -------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
ARG NPM_CONFIG_REGISTRY=https://registry.npmjs.org/
WORKDIR /app/web
COPY web/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --registry="${NPM_CONFIG_REGISTRY}"
COPY web/ .
RUN npm run build

# ---- Stage 2: backend (native compiler for TARGETPLATFORM) ----------------
FROM golang:1.25-alpine3.23 AS backend
ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=dev
ENV GOPROXY=${GOPROXY}
WORKDIR /app
RUN apk add --no-cache build-base
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-linkmode=external -s -w -X main.version=${VERSION}" -o mediastation-go ./cmd/server

# ---- Stage 3: runtime ------------------------------------------------------
FROM alpine:3.23 AS runtime-base
# Alpine ships ffprobe in the ffmpeg package; the application invokes ffprobe
# for media inspection and does not start ffmpeg.
RUN apk add --no-cache \
        ffmpeg \
        libwebp-dev \
        docker-cli \
        tzdata \
        ca-certificates \
        su-exec \
    && rm -rf /var/cache/apk/*

# Non-root user for the long-running process.
RUN addgroup -S mediastation && adduser -S mediastation -G mediastation

WORKDIR /app
COPY --from=backend /app/mediastation-go /usr/local/bin/mediastation-go
COPY --from=frontend /app/web/dist /app/web/dist

RUN mkdir -p /data /cache /media \
    && chown -R mediastation:mediastation /data /cache /media

# Default environment (overridable via docker-compose / `docker run -e`).
ENV MEDIASTATION_APP_PORT=8080 \
    MEDIASTATION_APP_DATA_DIR=/data \
    MEDIASTATION_APP_WEB_DIR=/app/web/dist \
    MEDIASTATION_CACHE_CACHE_DIR=/cache \
    MEDIASTATION_LOGGING_LEVEL=info \
    TZ=Asia/Shanghai

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD busybox wget -q --spider http://127.0.0.1:8080/api/health || exit 1

# Tiny entrypoint that lets us run as a NAS host UID/GID via PUID/PGID without
# rewriting /etc/passwd or /etc/group on every container start.
COPY docker-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

CMD ["/entrypoint.sh"]

# Optional HongGuo Android controller; default images do not include these tools.
FROM runtime-base AS runtime-android
RUN test "$(uname -m)" = x86_64 \
    && apk add --no-cache android-tools curl xz \
    && mkdir -p /opt/hongguo \
    && curl -fsSL --retry 3 https://github.com/frida/frida/releases/download/16.7.19/frida-inject-16.7.19-android-x86_64.xz -o /tmp/inject.xz \
    && echo '5067656da28620d7016ff63b9149c75e9f08ffcc1fcf393d5adce9b4adf52026  /tmp/inject.xz' | sha256sum -c - \
    && xz -dc /tmp/inject.xz > /opt/hongguo/frida-inject-android \
    && curl -fsSL --retry 3 https://lf9-apk.ugapk.cn/package/apk/novelread/12267_73932/novelread_seo_laxin_pc_android_v12267_73932_d587_1790246416.apk -o /opt/hongguo/hongguo.apk \
    && echo '1d668fcbd3f9547f173287a03b06dda0e34faad8228639b66b37faab4c5ec516  /opt/hongguo/hongguo.apk' | sha256sum -c - \
    && chmod 755 /opt/hongguo/frida-inject-android \
    && rm /tmp/inject.xz \
    && apk del curl xz

# Keep the normal multi-architecture image as the default build target.
FROM runtime-base AS runtime
