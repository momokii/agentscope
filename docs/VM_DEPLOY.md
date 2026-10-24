# VM-deploy runbook — opencode mission control (Wave 4, W4-3)

> Task: "Write VM deploy runbook with confirm checklist (W4-3)."

Audience: the human holding the VM keyboard (tailed-scale-reached,
zellij-multiplexed host). F2/F4 gates are executed from this doc.

## Label legend (read first)

- **VERIFIED-here** — proven on the dev/laptop host or in repo files during
  W4-1/W4-2. No need to re-prove; quoted exactly, not paraphrased.
- **TO-RUN-on-VM** — requires the TARGET VM (its XDG/UID/zellij/tailscale
  values, live fleet content, F2 matrix). **Nothing in this doc labeled
  TO-RUN was executed by the authoring agent** — every TO-RUN row is a
  human step. Tick each box as you complete it.

## 0. Prerequisites — target-VM values (all TO-RUN-on-VM)

Run every row on the VM **before** `docker compose up`. The "If wrong"
column names the exact loud startup failure it produces (copied verbatim
from `cmd/dashboard/selfcheck.go` / `cmd/dashboard/main.go` — the binary
prints `dashboard: FATAL: <err>` on stderr and exits non-zero; silent
degradation is banned by design).

| # | Command (copy-paste) | Good-output example (ASSUMPTION until you run it — record yours) | If wrong → FATAL text + blind-dashboard mechanism |
|---|---|---|---|
| P1 | `echo $XDG_RUNTIME_DIR` | `/run/user/1000` (must equal compose `XDG_RUNTIME_DIR: /run/user/${UID:-1000}` **and** the socket volume source `/run/user/${UID:-1000}/zellij`) | `XDG_RUNTIME_DIR is empty — export XDG_RUNTIME_DIR=/run/user/$(id -u) (verify the host value with \`echo $XDG_RUNTIME_DIR\`); the zellij CLI resolves its sockets from it`. A wrong-but-nonempty value passes this check and fails later as a **blind dashboard** (container CLI looks in the wrong dir, sees zero sessions). Cross-check: `ls $XDG_RUNTIME_DIR/zellij/contract_version_1/` must list live session sockets. |
| P2 | `id -u` and `id -g` | `1000` / `1000` (must equal compose `user: "${UID:-1000}:${GID:-1000}"` after your `export UID GID` — see deploy step D1) | `DATA_DIR /data/opencode unreadable: … — check the :ro mount and UID parity (container UID=%d vs host UID from \`id -u\`)` (also `open DATA_DIR … read-only: … — check the :ro mount and UID parity …` from `main.go`). Sockets are **0700** — same-user is effectively mandatory; a UID mismatch = blind dashboard even when the FATAL does not fire. |
| P3 | `zellij --version` | `zellij 0.44.3` — **major MUST be `0`**, matching the baked CLI (`Dockerfile`: `ZELLIJ_VERSION=0.44.3`, "MAJOR 0 … the baked CLI major MUST match the host's"). Socket contract dir is version-sensitive: `$XDG_RUNTIME_DIR/zellij/contract_version_1` | No dedicated major-skew FATAL (major is logged at boot: `slog.Info("zellij detected", …)`); skew surfaces as a **blind dashboard** — the container CLI cannot speak the host's socket contract, `list-sessions`/`list-panes` fail, zero panes shown. If majors differ, rebuild the image with a matching `ZELLIJ_VERSION` (same major) — do not proceed. |
| P4 | `docker --version` and `docker compose version` | `Docker Engine 2x.x` / `Compose v2.x` (any recent; W4-1 proven on `docker 29.3.1`) | No FATAL — compose itself refuses (`docker compose config` non-zero). Fix Docker before continuing. |
| P5 | `tailscale status` | Your tailnet peers listed, `Self` marked active; VM reachable from your laptop (`tailscale ping <vm>`) | No container FATAL — failure surfaces at step D8 (serve URL unreachable). Fix tailnet login/ACLs before exposing. Optional ACL pointer: `docs/SECURITY.md` §4 (restrict servicers to `tag:owner`/`tag:admin`). |

Also record once (TO-RUN-on-VM): `go version` (want `go1.27.x` — matches
`Dockerfile` `golang:1.27.3-bookworm` and `go.mod`) and `ls
~/.local/share/opencode/opencode.db` (must exist; the actual data dir).

**VERIFIED-here** (quoted from W4-1, do not re-tune): image
`opencode-dashboard:0.1.0`; `pid: "host"`; `restart: unless-stopped`;
container `PORT` fixed at `"8080"` (`EXPOSE 8080`, `HEALTHCHECK …
CMD ["/app/dashboard", "-healthcheck"]`, healthcheck `interval: 30s,
timeout: 5s, retries: 3, start_period: 15s`); publish
`"127.0.0.1:${PORT:-8080}:8080"`; mounts
`${HOME:-/home/hostuser}/.local/share/opencode:/data/opencode:ro`,
`${HOME:-/home/hostuser}/.cache/zellij:/data/zellij-cache:ro`,
`/run/user/${UID:-1000}/zellij:/run/user/${UID:-1000}/zellij:ro`;
env `DATA_DIR: /data/opencode`, `POLL_INTERVAL: ${POLL_INTERVAL:-10s}`.
Full proof log: `docs/DOCKER_VERIFY.md`.

## 1. Deploy (all TO-RUN-on-VM)

- [ ] **D1. Get the repo on the VM and set identity env.** Copy/clone this
  repo to the VM (any path, e.g. `~/opencode-dashboard`), then:
  ```bash
  cd ~/opencode-dashboard
  export UID                # re-exports bash's readonly $UID (equals `id -u`); do NOT assign it
  export GID="$(id -g)"     # GID is not special — set it explicitly (P2)
  export HOME="$HOME"       # expands the ${HOME:-/home/hostuser} volume sources to YOUR home
  export PORT=18081         # host-side port. 8080 was squatted on the laptop dev host;
                            # the same may apply here — any free 18xxx works. Container side stays 8080.
  docker compose config     # VERIFIED-here shape; TO-RUN: must exit 0 and show pid: host,
                            # 127.0.0.1:18081->8080, the three :ro mounts, the -healthcheck test
  ```
- [ ] **D2. Bring it up and wait out the warm-up.**
  ```bash
  PORT=18081 docker compose up -d --build
  sleep 20   # start_period (15s) + first polls; healthz `stale` may be true on the very first tick
  ```
- [ ] **D3. Healthy check.**
  ```bash
  docker compose ps   # want: dashboard container "running (healthy)"
  ```
- [ ] **D4. Health shape.**
  ```bash
  curl -s 127.0.0.1:$PORT/api/healthz
  # want paste-shape: {"healthy":true,"version":"0.1.0","db_path":"/data/opencode/opencode.db","last_poll_ms":<n>,"stale":false}
  ```
  (`stale:true` on the first tick only is acceptable — re-curl after one
  `POLL_INTERVAL`.) Field list frozen: `{healthy, version, db_path,
  last_poll_ms, stale}` (`internal/hub/handlers.go`).
- [ ] **D5. Fleet shape.**
  ```bash
  curl -s 127.0.0.1:$PORT/api/fleet | head -c 300; echo
  # want paste-shape: {"seq":<n>,"sessions":[...],"kpis":{...}}
  # empty sessions array is FINE on a host with no opencode sessions yet
  ```
- [ ] **D6. Loopback-only proof (paste both lines).**
  ```bash
  ss -tlnp | grep $PORT
  # want:  tcp  LISTEN  0  4096  127.0.0.1:18081  0.0.0.0:*  users:(...)
  # and NO line with 0.0.0.0:18081 — compose publishes 127.0.0.1 ONLY (SECURITY.md §4)
  ```
- [ ] **D7 (optional). Gate with Basic auth.** `docs/SECURITY.md` §3 owns
  this: `export DASHBOARD_AUTH='user:pass'` (split on the FIRST colon)
  before `up` (or add to compose `environment:` + `up` again). Empty/unset
  = explicit open tailnet-trust mode. Malformed (no colon, empty user/pass)
  = fail-closed refuse-to-serve. TO-RUN only if you want the gate.
- [ ] **D8. Expose to the tailnet — `serve`, NEVER `funnel`.**
  ```bash
  curl -s 127.0.0.1:$PORT/api/healthz   # re-confirm {"healthy":true,...} FIRST
  tailscale serve --bg --https=443 http://127.0.0.1:$PORT
  tailscale serve status                # confirm: serve ON, funnel OFF
  ```
  Open the shown `https://<vm-tailnet-name>` URL from your laptop browser —
  the dashboard must load. **⚠️ `tailscale funnel` is BANNED.** Funnel puts
  the dashboard on the **public internet**, where the v1 Basic-auth gate
  (base64 = reversible, not encryption) is the only control and
  tailnet identity/ACLs no longer bound the blast radius. Serve =
  tailnet-only; funnel = world-readable credentials path. (Posture:
  `docs/SECURITY.md` §4 — referenced, not duplicated.)

## 2. EXITED-session semantics — operator note (read, then TO-RUN: confirm in UI)

Resurrecting an EXITED session **RE-RUNS the recorded commands — it never
resumes the old processes.** EXITED rows (`zellij list-sessions` suffix
`(EXITED - attach to resurrect)`, no socket under `contract_version_1/`,
metadata only in `~/.cache/zellij/`) are serialized layouts, not live
state (research §1 nuance, zellij-org/zellij#3531, #4805). The dashboard
reports EXITED sessions honestly (dead, resurrectable-by-re-run) and v1
offers **no resurrect button** (read-only; deferred to
`docs/ACTIONS_DEFERRED.md`). Operator wording on the VM: *"that session is
gone; resurrect would start its commands fresh — unsaved TUI state is not
coming back."*

## 3. F2 manual truth matrix (all TO-RUN-on-VM — the F2 gate)

Set up each row, then check what the dashboard shows at the tailnet URL
(§1 D8) and tick. Fleet shape reference: `SessionRow` carries
`status | pane{CWD,exited} | orphan` (`SessionRow` per plan §"Collector API
contract"; orphan rows serve screen as **409** `{"error":…,"db_only":true,
"hint":"session has no live zellij pane; transcript and DB data remain
available"}`).

- [ ] **T1–T3. Three live zellij panes.** Setup: `zellij --session
  f2check` (new session), open 3 panes (`Ctrl-O n` ×2), run `opencode` in
  each on different projects (or the same project twice + one `sleep` pane
  to vary COMMAND). Verify host-side: `zellij --session f2check action
  list-panes --command` shows 3 rows; `ls
  $XDG_RUNTIME_DIR/zellij/contract_version_1/` contains `f2check`. Want:
  dashboard fleet lists all 3 panes live (running/idle per DB signals),
  each with correct per-pane CWD; `GET /api/sessions/:id/screen` → 200
  live text for a focused pane.
- [ ] **T4. One orphan (opencode outside zellij).** Setup: detach/exit to a
  plain SSH shell with **no** `$ZELLIJ` (`echo $ZELLIJ` must be empty — if
  set, `exit` until it is), `cd` to any project, run `opencode`, leave it
  idle. Verify host-side: `pgrep -af opencode` shows it, but no zellij pane
  lists it. Want: dashboard shows it flagged **orphan** (proc-scan union,
  research §7); its screen endpoint → **409 DB-only** notice (orphans have
  no `dump-screen` — honest, never a fake empty screen).
- [ ] **T5. One EXITED session.** Setup: `zellij --session f2doomed` (run
  one pane, detach with `Ctrl-O d`), then `zellij kill-session f2doomed`.
  Verify host-side: `zellij list-sessions -n` shows `f2doomed (EXITED -
  attach to resurrect)` and `ls
  $XDG_RUNTIME_DIR/zellij/contract_version_1/` does **not** contain it.
  Want: dashboard reports it EXITED/dead (never "running"); resurrect
  wording per §2. Cleanup after ticking: `zellij delete-session f2doomed`.
- [ ] **T6. One zombie (State Z shown dead).** Setup (sacrificial only —
  never on a real opencode): new pane `sleep 600`, note its PID (`echo
  $!`), from another pane `kill -9 <that-pid>`, then immediately `cat
  /proc/<pid>/status | grep State` → `State: Z (zombie)`. Want: dashboard
  treats `Z` as **dead** (research §3: zombie = dead, awaiting reaping;
  missing `/proc/<pid>` = fully exited). Timing-sensitive — the parent
  shell may reap quickly; re-do with a fresh `sleep` if you miss the
  window. Cleanup: close the pane.
- [ ] **T7. One CWD-vs-directory mismatch (both shown).** Setup: `mkdir -p
  /tmp/f2mis && cd /tmp/f2mis`, run `opencode` there and continue/resume an
  existing session whose recorded directory is a *different* project (so
  pane CWD ≠ DB `session.directory`). Verify host-side: `readlink
  /proc/<opencode-pid>/cwd` → `/tmp/f2mis` while the DB row says the other
  project. Want: dashboard shows **both** — pane CWD as "where it runs",
  DB directory as "what it works on" (research §7) — never silently one.

## 4. Troubleshooting (TO-RUN-on-VM lookups)

| Symptom | Check IN THIS ORDER (commands copy-paste) | Cause → fix |
|---|---|---|
| Blind dashboard (fleet empty / zero panes despite live sessions) | 1. `id -u` vs `docker exec <ctr> id -u` (must match; sockets 0700) 2. `echo $XDG_RUNTIME_DIR` vs `docker exec <ctr> printenv XDG_RUNTIME_DIR` (path parity; socket source must equal) 3. host `zellij --version` major vs image `docker exec <ctr> zellij --version` major (both `0`; socket dir `contract_version_1` is version-sensitive) 4. `ls $XDG_RUNTIME_DIR/zellij/contract_version_1/` (live names = liveness signal #1) | UID/XDG/major skew or wrong socket-dir mount. Any mismatch → `docker compose down`, fix env, `up -d --build` again. If majors differ, rebuild with matching `ZELLIJ_VERSION`. Startup FATALs for the loud cases: `DATA_DIR … unreadable … UID parity …`, `opencode.db missing at … mount the parent opencode/ dir …`, `zellij binary not found in PATH … blind to panes`, `XDG_RUNTIME_DIR is empty …` (§0 table) |
| Stale frames (`stale:true` persists, old data) | `docker inspect <ctr> --format '{{json .Mounts}}'` (want ONLY the §0 allowlist mounts, every one `:ro`); confirm the opencode mount is the **parent dir** (`…/opencode:/data/opencode:ro`), NOT the bare `opencode.db` file (else `-wal`/`-shm` sidecars are hidden → blind-but-healthy staleness, compose header WAL decision) | WAL sidecars invisible, or DB-budget pressure: the dev host DB hit **8.2 GB** (1953 sessions) where full `part.data LIKE` scans exceed 60 s — queries must stay indexed/sliced per `internal/db` allowlist (plan data strategy rule 5). Never fix by widening mounts. |
| Port taken (`address already in use`) | `ss -tlnp \| grep <PORT>` (find the squatter) | Host-side variance only: pick another free 18xxx (`PORT=18082 docker compose up -d --build`). Container side stays `8080` — never change it. (The laptop's own 8080 was squatted; same may apply on the VM.) |
| Screen 503 (`GET /api/sessions/:id/screen` → 503) | Expected pre-drill-down or wiring fault | 503 = `SetScreenSource` never wired (nil backend serves 503 `screen-unavailable`, `internal/hub/screen.go`); in this binary `main.go` wires `srv.SetScreenSource(source)` — a 503 here means the build predates wiring or the route hit an unwired test server. Orphan panes instead return **409** DB-only (not 503) — see T4. |

## 5. Teardown (TO-RUN-on-VM)

```bash
docker compose down
ss -tlnp | grep $PORT   # want: NO output (nothing listening)
docker compose ps       # want: no running containers
```
All mounts are host bind-mounts — no volumes to prune (`docs/DOCKER_VERIFY.md` §3).

## Sources (every claim traces to one of these)

- W4-1 landed files: `docker-compose.yml` (env/mount/port/healthcheck
  values quoted in §0–§1), `Dockerfile` (Go 1.27.3, zellij 0.44.3 major 0,
  distroless `static-debian12:nonroot`), `docs/DOCKER_VERIFY.md` (container
  proof: healthz 200 shape, fleet shape, 127.0.0.1-only `ss`, clean
  teardown).
- W4-2: `docs/SECURITY.md` §§1–4, 7 (threat model, 4-row mount allowlist,
  secret inventory, 127.0.0.1 ×3 layers, serve-not-funnel + ACL pointer,
  fail-closed `DASHBOARD_AUTH`).
- Startup FATAL strings: `cmd/dashboard/selfcheck.go` (XDG empty, DATA_DIR
  unreadable, opencode.db missing, zellij not found / --version failed),
  `cmd/dashboard/main.go` (`dashboard: FATAL:` prefix, `127.0.0.1 ONLY —
  front with tailscale serve, never funnel` loopback assert,
  `SetScreenSource(source)` wiring).
- Frozen API shapes: `internal/hub/handlers.go`
  (`{healthy,version,db_path,last_poll_ms,stale}`, `{seq,sessions,kpis}`),
  `internal/hub/screen.go` (409 orphan body + hint, 503-while-nil).
- Plan: `.omo/plans/opencode-dashboard.md` W4-3 (this doc's acceptance:
  named human runs VM steps, pastes `healthz`) + F2 (runtime-truth gate
  executed via §3) + F4 (security/deploy gate) + Docker spec + data
  strategy rule 5 (8.2 GB budget).
- Research: `.omo/notepads/opencode-dashboard/
  research-runtime-observability.md` §1 (server PPID 1, EXITED = re-run not
  resume), §4 (plug-n-play compose, UID/XDG parity, 0700 same-user rule),
  §5 (serve-vs-funnel, auth.json never mounted), §7 (orphans, CWD
  mismatch, zombie-Z, EXITED pruning).
