# syntax=docker/dockerfile:1
FROM golang:1.26.6-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o /out/vidscribe .

FROM ghcr.io/astral-sh/uv:0.10.9 AS uv

FROM node:24-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=uv /uv /uvx /usr/local/bin/
COPY --from=build /out/vidscribe /usr/local/bin/vidscribe

RUN groupadd --system --gid 10001 vidscribe \
    && useradd --system --uid 10001 --gid vidscribe --home-dir /data --shell /usr/sbin/nologin vidscribe \
    && mkdir -p /data /work \
    && chown -R vidscribe:vidscribe /data /work

USER 10001:10001
WORKDIR /work
ENV HOME=/data \
    XDG_CACHE_HOME=/data/cache \
    UV_CACHE_DIR=/data/cache/uv \
    PYTHONUNBUFFERED=1

VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["node", "-e", "fetch('http://127.0.0.1:8080/healthz').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"]

ENTRYPOINT ["vidscribe"]
CMD ["serve", "--listen", ":8080", "--data-dir", "/data", "--max-runtime", "6h"]
