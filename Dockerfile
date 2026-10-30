# syntax=docker/dockerfile:1
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Two images (ADR 0009):
#   --target app   the Go binary (serve, migrate, healthcheck)
#   --target web   Caddy with the built SPA and the production Caddyfile

FROM docker.io/library/golang:1.26 AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
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
