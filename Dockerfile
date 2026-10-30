# syntax=docker/dockerfile:1
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Images (ADR 0009):
#   --target app     the Go binary (serve, worker, migrate, healthcheck)
#   --target web     Caddy with the built SPA and the production Caddyfile
#   --target backup  the network-less database backup container (ADR 0016)

FROM docker.io/library/golang:1.26 AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
# nodynamic: image codecs use their memory-safe Go builds only (ADR 0021).
RUN CGO_ENABLED=0 go build -trimpath -tags nodynamic \
    -ldflags "-s -w -X github.com/chmbrb/PetCommunityProject/internal/version.Version=${VERSION}" \
    -o /out/petcommunity ./cmd/petcommunity

FROM gcr.io/distroless/static-debian13:nonroot AS app
COPY --from=go-build /out/petcommunity /usr/local/bin/petcommunity
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/petcommunity"]
CMD ["serve"]

FROM docker.io/library/node:26-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
# Install the pnpm version pinned in package.json's packageManager field. Uses npm rather
# than corepack, which Node.js no longer bundles from version 25.
RUN npm install -g "$(node -p 'require("./package.json").packageManager')" \
    && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM docker.io/library/caddy:2.11-alpine AS web
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=web-build /src/web/dist /srv

# WAL-G, downloaded for the target architecture and checked against its published
# SHA-256 (ADR 0016).
FROM docker.io/library/alpine:3.24 AS walg
ARG TARGETARCH
ARG WALG_VERSION=v3.0.9
RUN case "$TARGETARCH" in \
      amd64) arch=amd64; sum=444ce216230c254edef803e098a59a1746806da3e0d55671d7b0f0082db14ef4 ;; \
      arm64) arch=aarch64; sum=cb37c5f8391bd8ef2efceeb9c6995059c0abf2fa74b9b717b42a16a7c93cfde1 ;; \
      *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && wget -qO /walg.tgz "https://github.com/wal-g/wal-g/releases/download/${WALG_VERSION}/wal-g-pg-24.04-${arch}.tar.gz" \
    && echo "${sum}  /walg.tgz" | sha256sum -c - \
    && tar -xzf /walg.tgz -C / \
    && mv "/wal-g-pg-24.04-${arch}" /wal-g

# --target backup: the network-less backup container. It's the PostGIS image (the
# same Postgres version as the database, for restores and drills) plus WAL-G.
FROM docker.io/postgis/postgis:18-3.6 AS backup
COPY --from=walg /wal-g /usr/local/bin/wal-g
COPY deploy/backup/pc-backup /usr/local/bin/pc-backup
# Scratch space for restore drills; a new volume mounted here inherits this owner.
RUN mkdir /scratch && chown postgres:postgres /scratch
USER postgres
ENTRYPOINT ["/usr/local/bin/pc-backup"]
CMD ["run"]
