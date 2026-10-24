# Docker verify runbook (W4-1, host-network revision)

Builds and proves the single-container deployment. The container shares the
host network namespace (`network_mode: "host"`): the Go 127.0.0.1 bind lands
on HOST loopback directly — there is no `ports:` stanza and no NAT hop.
The old bridge-publish model (`127.0.0.1:${PORT:-8080}:8080`) is SUPERSEDED:
bridge DNAT targets the container eth0 IP, which a container-loopback bind
never answers ("healthy but unreachable" — green in-netns healthcheck, RST
from the host). Never 0.0.0.0.

## 0. Prerequisites (one-time, on the TARGET machine)

```sh
go version                 # want: go1.27.x
zellij --version           # want: major 0 (record the exact version)
id -u && echo $XDG_RUNTIME_DIR
docker --version && docker compose version
pnpm --dir web build       # refresh web/dist before docker build
```

## 1. Validate the compose file (no daemon work)

```sh
docker compose config
```

Must exit 0 and show: `network_mode: host`, `pid: host`,
`user: "<uid>:<gid>"`, `PORT: "<port>"` (the ACTUAL listen port — host mode
has no mapping), the three `:ro` mounts, the
`["CMD", "/app/dashboard", "-healthcheck"]` healthcheck — and NO `ports:`
stanza (ignored under host mode; its presence would be a lie).

## 2. Full cycle

```sh
PORT=18085 docker compose up -d --build
sleep 20   # let start_period + first polls land; $PORT is the listen port
curl -s 127.0.0.1:${PORT:-8080}/api/healthz
curl -s 127.0.0.1:${PORT:-8080}/api/fleet | head -c 300; echo
ss -tlnp | grep ${PORT:-8080}
docker compose down
```

## 3. Expected results

- `healthz` → `{"healthy":true,"version":"0.1.0",...,"stale":false}` (stale may
  be true on the very first tick while the collector warms up).
- `fleet` → `{"seq":N,"sessions":[...],"kpis":{...}}` (empty sessions array
  is fine on a host with no opencode sessions).
- `ss` shows `127.0.0.1:${PORT}` LISTEN served directly by the dashboard
  (host mode: the binary holds host loopback itself — there is NO
  `docker-proxy` line) and NO `0.0.0.0:${PORT}` line.
- `docker compose down` removes the container; no volumes to prune
  (all mounts are host bind-mounts).

## 4. Troubleshooting

| Symptom | Cause → fix |
|---|---|
| `DATA_DIR ... unreadable` FATAL | `:ro` mount missing or UID mismatch → check `user:` vs host `id -u` |
| `opencode.db missing` FATAL | Wrong HOME substitution → set `HOME` to the session owner's home |
| `zellij binary not found` FATAL | Image built without the zellij stage → rebuild (`--build`) |
| `XDG_RUNTIME_DIR is empty` FATAL | Env not passed → check compose `environment` interpolation |
| Fleet shows zero panes despite live sessions | Socket dir mismatch → `echo $XDG_RUNTIME_DIR` on host must equal the container value; UID must match (sockets are 0700) |
