#!/usr/bin/env bash
# opencode mission control — one-click deploy (deploy-ux).
#
# Fresh checkout → verified-running dashboard in one command:
#   ./deploy.sh              # up on 8080 (or next free port), build, verify, banner
#   ./deploy.sh --port 18081 # pin the listen port (host mode: the binary binds
#                             host 127.0.0.1:$PORT directly — no mapping)
#   ./deploy.sh --down        # teardown + verify clean
#
# Encodes the proven W4-1 manual cycle (docs/DOCKER_VERIFY.md) with hardening:
# squatted ports auto-bump (8080 was squatted on the dev host), real `curl`
# (not wrapper aliases) for proof pastes, first-tick stale:true tolerated.
set -euo pipefail

# ---- pinned versions (must match Dockerfile + docs/VM_DEPLOY.md §0) ----
BAKED_ZELLIJ="0.44.3"   # Dockerfile ARG ZELLIJ_VERSION; socket contract is major-sensitive
BAKED_GO="1.27.3"       # Dockerfile golang:1.27.3-bookworm + go.mod
BAKED_IMAGE="opencode-dashboard:1.1.0"

PORT="${PORT:-8080}"
DOWN=0

usage() {
  cat <<'EOF'
Usage: ./deploy.sh [--port N] [--down] [--help]

  --port N   listen port (default 8080, or $PORT). Host networking: the
             binary binds host 127.0.0.1:$PORT directly — no container/host
             split. If occupied, auto-bumps to the next free port and says so.
  --down     tear down the stack and verify nothing listens / nothing runs.
  --help     this text.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="${2:?--port needs a value}"; shift 2 ;;
    --down) DOWN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
  esac
done

log()  { printf 'deploy: %s\n' "$*"; }
fail() { printf 'deploy: FATAL: %s\n' "$*" >&2; exit 1; }

port_in_use() { # $1=port → 0 if anything LISTENs on it (any bind addr)
  ss -tln 2>/dev/null | grep -qE "[:.]$1[[:space:]]"
}

next_free_port() { # $1=start → echoes first free port at/after start (cap +100)
  local p="$1" i=0
  while port_in_use "$p"; do
    p=$((p + 1)); i=$((i + 1))
    [ "$i" -gt 100 ] && fail "no free port near $1 — free one with: ss -tlnp | grep $1"
  done
  echo "$p"
}

need_cmd() { # $1=cmd $2=fix-hint
  command -v "$1" >/dev/null 2>&1 || fail "missing '$1'. Fix: $2"
}

# ---------------- prerequisites (loud fix hints, copy-pasteable) ----------------
need_cmd docker "install Docker: https://docs.docker.com/engine/install/ then 'sudo usermod -aG docker \$USER' + re-login"
docker compose version >/dev/null 2>&1 \
  || fail "need Compose v2 ('docker compose version' failed). Fix: install Docker Engine 20.10+ with the compose plugin — modern 'docker compose' is required, there is no docker-compose v1 fallback"
need_cmd curl "install curl: sudo apt install curl"
need_cmd ss "install iproute2: sudo apt install iproute2"
need_cmd chmod "install coreutils: sudo apt install coreutils"
id -u >/dev/null 2>&1 \
  || fail "'id -u' failed — cannot determine UID for compose user: parity. Fix: log in as a real user (not a bare container shell)"
[ -n "${XDG_RUNTIME_DIR:-}" ] \
  || fail "XDG_RUNTIME_DIR is empty. Fix: export XDG_RUNTIME_DIR=/run/user/\$(id -u)  (verify with: echo \$XDG_RUNTIME_DIR)"
if command -v zellij >/dev/null 2>&1; then
  HOST_Z="$(zellij --version 2>/dev/null || true)"
  HOST_MAJOR="$(printf '%s' "$HOST_Z" | awk '{print $2}' | cut -d. -f1)"
  BAKED_MAJOR="$(printf '%s' "$BAKED_ZELLIJ" | cut -d. -f1)"
  if [ -n "$HOST_MAJOR" ] && [ "$HOST_MAJOR" != "$BAKED_MAJOR" ]; then
    log "WARN: host zellij major ($HOST_MAJOR, '$HOST_Z') != baked major ($BAKED_MAJOR, zellij $BAKED_ZELLIJ). Continuing — skew surfaces as a BLIND dashboard (zero panes); rebuild the image with a matching ZELLIJ_VERSION if fleet is empty."
  else
    log "zellij OK: $HOST_Z (baked $BAKED_ZELLIJ)"
  fi
else
  log "WARN: 'zellij --version' not found on host — cannot check major skew vs baked $BAKED_ZELLIJ. Continuing (container CLI still runs); a blind/empty fleet usually means this skew. Fix: install zellij major ${BAKED_ZELLIJ%%.*} or rebuild the image to match your host."
fi

# ---------------- teardown mode ----------------
# Port-agnostic: --down without --port tears down whatever is running.
# A squatted port held by ANOTHER process is not our failure (the old
# raw-port check failed teardowns for unrelated squatters) — fail only if
# the dashboard's own listener or containers survive.
if [ "$DOWN" -eq 1 ]; then
  log "tearing down (docker compose down)…"
  docker compose down
  if [ -n "$(docker compose ps -q 2>/dev/null || true)" ]; then
    fail "'docker compose ps -q' still lists containers. Fix: docker compose ps ; docker compose down --remove-orphans"
  fi
  if ss -tlnp 2>/dev/null | grep -q 'dashboard'; then
    fail "a dashboard-owned listener survived down. Fix: ss -tlnp | grep dashboard  then stop that process"
  fi
  if port_in_use "$PORT"; then
    log "note: :$PORT still LISTENs, but no dashboard container or listener remains — an unrelated squatter holds it (not our stack). Bye."
  else
    log "clean: nothing on :$PORT (ss empty), no containers (compose ps empty). Bye."
  fi
  exit 0
fi

# ---------------- port: occupied → auto-bump, loudly ----------------
if port_in_use "$PORT"; then
  FREE="$(next_free_port "$PORT")"
  log "port $PORT is squatted (ss shows a listener) — auto-bumping to $FREE. Override: ./deploy.sh --port <N>"
  PORT="$FREE"
fi
export PORT

# ---------------- web/dist: build if missing OR stale (committed dist is the fallback) ----------------
# Staleness rule: any web/src/** newer than web/dist/index.html → rebuild.
# NOTE: every run rebuilds the image (`up --build` below) — a previous
# revision skipped rebuilds on healthy stacks, which served stale code
# after `git pull` and confused verification. Rebuilds are layer-cached,
# so the no-change case costs seconds, never correctness.
NEED_BUILD=0
if [ ! -f web/dist/index.html ]; then
  NEED_BUILD=1; log "web/dist missing — will build."
elif [ -n "$(find web/src -type f -newer web/dist/index.html -print -quit 2>/dev/null)" ]; then
  NEED_BUILD=1; log "web/src newer than web/dist — will rebuild (stale dist)."
else
  log "web/dist fresh — reusing committed dist (no Node needed)."
fi
if [ "$NEED_BUILD" -eq 1 ]; then
  command -v npm >/dev/null 2>&1 \
    || fail "web/dist missing/stale but 'npm' not found. Fix: install Node 20+ (https://nodejs.org/) then re-run — or restore committed web/dist via git."
  log "building SPA: npm --prefix web ci && npm run build --prefix web"
  npm --prefix web ci
  npm run build --prefix web
fi

# ---------------- up ----------------
# UID is a readonly bash var — re-export as-is (= `id -u`); GID is set explicitly.
export UID
export GID="$(id -g)"
export HOME="$HOME"

# ---------------- timezone: forward the host zone so "today" is local midnight ----------------
# Without TZ the container is UTC and cost_today starts hours early (WIB host:
# 7h skew). /etc/localtime:ro is also mounted by compose, so Go still resolves
# the zone if this leaves TZ unset — but an explicit value is what we print.
if [ -z "${TZ:-}" ]; then
  DETECTED_TZ="$(cat /etc/timezone 2>/dev/null | tr -d '[:space:]' || true)"
  if [ -z "$DETECTED_TZ" ] && command -v timedatectl >/dev/null 2>&1; then
    DETECTED_TZ="$(timedatectl show -p Timezone --value 2>/dev/null | tr -d '[:space:]' || true)"
  fi
  if [ -n "$DETECTED_TZ" ]; then export TZ="$DETECTED_TZ"; fi
fi
# Forward the full operator-tunable env so compose picks it up (compose
# defaults cover direct `docker compose` use; explicit exports win).
export XDG_RUNTIME_DIR
if [ -n "${DB_TIMEOUT:-}" ]; then export DB_TIMEOUT; fi
if [ -n "${ACTIVE_WINDOW:-}" ]; then export ACTIVE_WINDOW; fi
log "bringing up: UID=$UID GID=$GID PORT=$PORT TZ=${TZ:-unset} (host mode: binds host 127.0.0.1:$PORT directly)"
docker compose up -d --build

# ---------------- wait for healthy (≤90s) ----------------
log "waiting for container health (≤90s)…"
CID="$(docker compose ps -q dashboard)"
[ -n "$CID" ] || fail "no dashboard container after up. Fix: docker compose ps ; docker compose logs dashboard | tail -50"
for i in $(seq 1 90); do
  HST="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$CID" 2>/dev/null || echo unknown)"
  if [ "$HST" = "healthy" ]; then log "healthy after ~${i}s"; break; fi
  if [ "$i" -eq 90 ]; then
    docker logs --tail 50 "$CID" >&2 || true
    fail "container not healthy after 90s (last: $HST). Logs tailed above. Fix: docker compose logs dashboard | tail -100"
  fi
  sleep 1
done

# ---------------- live proof: real curl binary, pasted output ----------------
# Auth-aware proof curls: when DASHBOARD_AUTH=user:pass is set the gate
# covers EVERYTHING (API + SPA), so proofs must present the same creds.
# curl -u splits on the first colon, mirroring the server/probe contract
# (passwords may contain colons). The value is never echoed (banner below
# prints set/unset only — that redaction invariant is load-bearing).
auth_curl() { # usage: auth_curl <curl-args...>
  if [ -n "${DASHBOARD_AUTH:-}" ]; then
    curl -s -u "$DASHBOARD_AUTH" "$@"
  else
    curl -s "$@"
  fi
}
log "--- proof: GET /api/healthz ---"
HEALTHZ="$(auth_curl "http://127.0.0.1:$PORT/api/healthz" || true)"
printf '%s\n' "$HEALTHZ"
echo "$HEALTHZ" | grep -q '"healthy":true' \
  || {
    # First-tick stale:true is NORMAL on huge DBs (8.2GB dev host) — one more
    # poll interval, then re-check rather than failing the deploy.
    log "healthz not healthy:true on first read (stale warm-up is normal) — waiting one POLL_INTERVAL and re-checking…"
    sleep "${POLL_INTERVAL:-10s}" 2>/dev/null || sleep 10
    HEALTHZ="$(auth_curl "http://127.0.0.1:$PORT/api/healthz" || true)"
    printf '%s\n' "$HEALTHZ"
    echo "$HEALTHZ" | grep -q '"healthy":true' \
      || { docker logs --tail 30 "$CID" >&2 || true
           fail "healthz never reached healthy:true (empty/reset = app unreachable on host loopback, NOT warm-up). Logs tailed above. Fix: docker compose logs dashboard | tail -100"; }
  }
log "--- proof: GET /api/fleet (head -c 300) ---"
auth_curl "http://127.0.0.1:$PORT/api/fleet" 2>/dev/null | head -c 300 || true; echo
log "--- proof: SPA served at / ---"
auth_curl -o /dev/null -w 'GET / -> %{http_code}\n' "http://127.0.0.1:$PORT/" || true
if [ "${BIND_ADDR:-127.0.0.1}" = "127.0.0.1" ] || [ "${BIND_ADDR:-127.0.0.1}" = "::1" ] || [ "${BIND_ADDR:-127.0.0.1}" = "localhost" ]; then
  log "--- proof: loopback-only (want 127.0.0.1, NO 0.0.0.0:$PORT, NO docker-proxy) ---"
  ss -tlnp | grep "$PORT" || fail "ss shows nothing on $PORT?! Fix: docker compose ps ; docker compose logs dashboard | tail -20"
  if ss -tln 2>/dev/null | grep -qE "(0\.0\.0\.0|\*|:::):$PORT[[:space:]]"; then
    fail "port $PORT bound off-loopback — loopback-only violated. Fix: ss -tlnp | grep $PORT to find the offender (the dashboard itself can never do this: Go asserts loopback, compose has no ports: stanza)"
  fi
else
  # Explicit non-loopback opt-in (binary refuses to serve it without valid
  # DASHBOARD_AUTH): skip the loopback assertion, keep the docker-proxy
  # check (host mode must bind the dashboard binary directly).
  log "--- proof: non-loopback opt-in (BIND_ADDR=$BIND_ADDR, auth-gated) ---"
  ss -tlnp | grep "$PORT" || fail "ss shows nothing on $PORT?! Fix: docker compose ps ; docker compose logs dashboard | tail -20"
fi
if ss -tlnp 2>/dev/null | grep "$PORT" | grep -qi 'docker-proxy'; then
  fail "a docker-proxy holds :$PORT — host mode expects the dashboard binary directly. Fix: docker compose config | grep -E 'network_mode|ports:' must show host mode with no ports: stanza"
fi

# ---------------- banner ----------------
if [ -n "${DASHBOARD_AUTH:-}" ]; then AUTH_STATUS="yes (set)"; else AUTH_STATUS="no (unset = open tailnet-trust mode)"; fi
cat <<EOF

================ opencode mission control: UP ================
  URL:            http://127.0.0.1:$PORT
  healthz:        $HEALTHZ
  tailnet next:   tailscale serve --bg --https=443 http://127.0.0.1:$PORT
                  (funnel is BANNED: it puts a reversible base64 Basic gate on the public internet — serve stays tailnet-only.)
  DASHBOARD_AUTH: $AUTH_STATUS
  tz:             ${TZ:-unset (container stays UTC; /etc/localtime mount still applies)}
  pinned:         image $BAKED_IMAGE · Go $BAKED_GO · zellij $BAKED_ZELLIJ · distroless static-debian12:nonroot
  teardown:       ./deploy.sh --port $PORT --down
================================================================
EOF
