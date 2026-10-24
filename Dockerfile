# opencode mission control — single-binary deployable (Wave W4-1).
#
# Pipeline: Go build (embeds web/dist) + pinned zellij CLI → distroless
# static runtime. No Node at runtime (or at build: web/dist is built by the
# web pipeline via `pnpm --dir web build` and committed before docker build).
#
# Pinned versions (recorded 2026-10-24 on the dev host):
#   - Go 1.27.3  (matches go.mod + host `go version go1.27.3`)
#   - zellij 0.44.3, MAJOR 0 (matches host `zellij --version`;
#     socket contract dir contract_version_1 is version-sensitive, so the
#     baked CLI major MUST match the host's — the entrypoint logs it at boot)
#
# UID strategy: NO UID is baked into this image. The runtime user comes from
# compose `user: "${UID:-1000}:${GID:-1000}"` so the container runs as the
# host user (zellij sockets are 0700 — same-user is effectively mandatory).

# syntax=docker/dockerfile:1

# ---- Stage 0: fetch the pinned zellij CLI (musl static: runs on scratch) ----
FROM golang:1.27.3-bookworm AS zellij-fetch
ARG ZELLIJ_VERSION=0.44.3
ADD https://github.com/zellij-org/zellij/releases/download/v${ZELLIJ_VERSION}/zellij-x86_64-unknown-linux-musl.tar.gz /tmp/zellij.tar.gz
RUN tar -xzf /tmp/zellij.tar.gz -C /tmp/ zellij \
    && rm /tmp/zellij.tar.gz \
    && /tmp/zellij --version
# ^^^ The trailing --version is the in-image smoke check: the baked CLI must
# run, and its major must match the host's (see entrypoint self-check).

# ---- Stage 1: build the single binary (web/dist embedded, CGO off) ----
FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# web/dist must exist in the build context (committed by the web pipeline).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dashboard ./cmd/dashboard

# ---- Stage 2: distroless static runtime (no shell, no package manager) ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dashboard /app/dashboard
COPY --from=zellij-fetch /tmp/zellij /usr/local/bin/zellij
ENV PATH="/usr/local/bin:${PATH}"
# EXPOSE is documentation only: the actual publish (127.0.0.1-bound) happens
# in docker-compose.yml ports. Never publish this port on 0.0.0.0.
EXPOSE 8080
ENTRYPOINT ["/app/dashboard"]
# Exec-form HEALTHCHECK (no shell in distroless): the binary probes its own
# /api/healthz via the -healthcheck flag (127.0.0.1 + $PORT, 5s timeout).
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/app/dashboard", "-healthcheck"]
