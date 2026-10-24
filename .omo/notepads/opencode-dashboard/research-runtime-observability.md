# Research: zellij / VM / Docker runtime observability for host-monitoring dashboard

Date: 2026-10-24. Environment: laptop dev machine (NOT the target VM —
tailscale absent here; VM-specific items marked UNVERIFIED where applicable).
zellij 0.44.3, docker 29.3.1 (cgroup v2), opencode with ~/.local/share/opencode/opencode.db (1953 sessions, 13 projects).

## 1. Core belief: "zellij sessions keep running after disconnect until destroyed" — TRUE (with one nuance)

Process model observed live on THIS machine (`ps -ef --forest`, VERIFIED):

```
zellij --server /run/user/1000/zellij/contract_version_1/personal-laptop-2  (PPID=1, daemon)
├─ /usr/bin/zsh            (pane, pts/2)
│  └─ opencode             (PPID=pane shell; cwd=/home/.../opencode-dashboard)
│     ├─ node .../lsp-daemon/dist/cli.js mcp
│     └─ bun .../mcp-server.cjs
├─ /usr/bin/zsh            (pane, pts/5) └─ opencode ...
└─ /usr/bin/zsh            (pane, pts/4) └─ opencode ...
zellij attach personal-laptop-2   (CLIENT, child of sshd/zsh — dies on disconnect)
```

- The **server is daemonized (PPID 1)** at session creation. The `zellij attach`
  process is just a rendering client. Closing SSH / dropping tailscale kills only
  the client; server + pane shells + `opencode` children are untouched. That is the
  entire technical reason sessions "survive disconnect".
- Detach (Ctrl-O d or `zellij action detach`) vs network drop are equivalent from
  the server's viewpoint: client gone, server keeps running.
- NUANCE (cited, zellij-org/zellij#3531, #4805): if the server itself **quits,
  crashes, or the machine reboots**, panes' processes are NOT persisted — the
  session becomes EXITED and resurrection only **re-runs** the recorded commands
  from a serialized layout. So "keeps running" holds for disconnect/detach, NOT
  for server death. Distinguish `list-sessions` rows with no suffix (live server)
  from `(EXITED - attach to resurrect)` rows (serialized metadata only).

## 2. Zellij observability primitives (all VERIFIED live, zellij 0.44.3)

| Need | Command | Notes / real output seen |
|---|---|---|
| List sessions (parseable) | `zellij list-sessions -n` / `-s` | `-n` strips color; `-s` names only. EXITED rows carry `(EXITED - attach to resurrect)` suffix — parse that as dead. |
| Per-session panes+tabs | `zellij --session <name> action list-panes --all --command` | Cross-session query works from any shell. Columns: TAB_ID TAB_POS TAB_NAME PANE_ID TYPE TITLE COMMAND CWD FOCUSED FLOATING EXITED + geometry. `COMMAND`=`opencode`, `CWD`=project dir, `TITLE`=`OC \| ...` for opencode panes vs shell prompt text for plain panes. |
| Machine-readable | `zellij action list-panes -j` | JSON: `terminal_command`, `exited`, `exit_status`, `is_floating/focused/fullscreen`, `tab_id/tab_name`, geometry, `cursor_coordinates_in_pane`. Primary poll API for dashboard. |
| Tabs/clients | `zellij action list-tabs`, `query-tab-names`, `current-tab-info`, `action list-clients` | `list-clients` shows CLIENT_ID + focused pane + RUNNING_COMMAND. |
| Live pane content | `zellij action dump-screen --pane-id <id> [--full] [--path F \| stdout] [-a]` | VERIFIED: dumped live opencode TUI text of pane 3 to stdout. `--full` = with scrollback. This is the "screenshot-as-text" primitive. |
| Layout snapshot | `zellij action dump-layout` | KDL with `pane command="opencode" cwd="..."` per pane — good for resurrect-oriented views. |
| Streaming | `zellij subscribe --pane-id <id> [-f json] [-s scrollback]` | Streams render updates for a pane. Basis for event-driven arch (see §6). Help VERIFIED; sustained streaming UNVERIFIED (did not hold a subscription open). |
| Plugin/event bus | `zellij pipe --name <pipe> [--plugin <wasm>]` | Arbitrary data bus to plugins; alternative push channel. Cited from `--help` (VERIFIED help text), end-to-end use UNVERIFIED. |
| Web client | `zellij web` (built-in, serves 127.0.0.1:8082 default) | Cited from zellij.dev tutorial (2025-03). Would give browser-native session sharing, but it serves full interactive terminals — evaluate separately; NOT recommended as dashboard data source. UNVERIFIED (did not start it). |

Socket/IPC paths (VERIFIED via /proc/<zserver-pid>/cmdline + Read tool):
- Live server socket: `/run/user/1000/zellij/contract_version_1/<session-name>`
  (i.e. `$XDG_RUNTIME_DIR/zellij/contract_version_1/<session>`; uid 1000 here).
  Directory listing shows ONLY live sessions (`personal-laptop-2`, `work-ptg`);
  EXITED sessions have no entry. => socket-dir listing = liveness signal #1.
- Serialized EXITED sessions: `~/.cache/zellij/` UUID dirs (13 seen) + version dir
  `~/.cache/zellij/0.44.3/` — resurrection store. Layout of cache internals UNVERIFIED.
- `/tmp/zellij-<uid>/` holds only `zellij-log/` here — NOT the socket. Do not mount it expecting IPC.
- Config: `~/.config/zellij/config.kdl`. `session_serialization false` disables
  resurrection (cited, zellij-org/zellij#4641).

## 3. Process mapping: pane -> opencode PID -> liveness (VERIFIED)

- Pane shell PID → `opencode` child via PPID (`ps -o pid,ppid,cmd`, `/proc/<pid>/status` PPid field).
- CWD ground truth: `readlink /proc/<opencode-pid>/cwd` → project dir
  (VERIFIED: `.../opencode-dashboard` for both pane shell 3371384 and opencode 3850783).
- Liveness states from `/proc/<pid>/status` `State:` field:
  `R` = on-CPU (busy), `S` = sleeping (idle, waiting on input — the normal opencode
  steady state, VERIFIED `State: S` for live opencode 3850783), `D` = uninterruptible
  IO, `Z` = zombie (dead, awaiting reaping — parent pane shell still holds it),
  missing `/proc/<pid>` = fully exited.
- Zombie nuance: a zombie opencode still has a PPID entry; dashboard must treat
  `Z` as dead. Poll `/proc/<pid>/stat` field 3 or `status` State.
- "Still running after disconnect": nothing re-parents (server already PPID 1);
  panes keep their pts; opencode never receives SIGHUP because its parent chain
  (pane shell ← zellij server) never dies. No nohup/tmux-style signal shielding
  needed — it falls out of the daemonized server design.

## 4. Docker plug-n-play design (container on SAME VM observes HOST)

Reference pattern: Netdata's documented docker run (`--pid=host` + host mounts).
Docker `--pid=host` + bind-mount semantics cited from docs.docker.com
(run reference, bind-mounts, host network driver). Container-side behavior below
is standard Linux/docker semantics; end-to-end compose on the TARGET VM is UNVERIFIED.

Minimal host inputs the dashboard needs:
- (a) opencode session DATA: `~/.local/share/opencode/` (opencode.db + storage/).
  Default path: `/home/<user>/.local/share/opencode`. Read-only mount suffices.
- (b) zellij session LIST + live content: working `zellij` CLI **inside the image**
  talking to the host socket dir. Mount `/run/user/<uid>/zellij` (host) →
  same path in container AND set `XDG_RUNTIME_DIR=/run/user/<uid>` + run container
  as the same UID (see permissions). The CLI resolves sockets from XDG_RUNTIME_DIR,
  so path parity matters. Alternative without zellij binary: parse socket-dir
  listing for liveness only (names = live sessions), but pane detail then needs CLI.
- (c) process LIVENESS: `--pid=host` (container joins host PID namespace; host PIDs
  visible 1:1, VERIFIED semantics per docker docs) and optionally read-only
  `-v /proc:/host/proc:ro` as fallback ground truth for auditing. With pid=host,
  plain `/proc/<pid>` works directly.

Plug-n-play compose sketch (target VM must substitute UID/home; principle VERIFIED, file UNVERIFIED):

```yaml
services:
  dashboard:
    pid: "host"                       # share host PID namespace (liveness)
    user: "1000:1000"                 # match host user (file ownership)
    environment:
      XDG_RUNTIME_DIR: /run/user/1000 # must match host uid's runtime dir
      HOME: /home/hostuser
    volumes:
      - /run/user/1000/zellij:/run/user/1000/zellij:ro
      - /home/hostuser/.local/share/opencode:/data/opencode:ro
      - /home/hostuser/.cache/zellij:/data/zellij-cache:ro   # EXITED/resurrect info
    ports: ["127.0.0.1:PORT:PORT"]    # never 0.0.0.0 (see §5)
```

Zero-manual-wiring notes: bake `zellij` binary (same major version as host —
socket contract dir `contract_version_1` is version-sensitive) into the image;
auto-detect UID via `${UID}` at `docker run` time; default paths above ARE the
upstream defaults (opencode XDG data dir, zellij XDG_RUNTIME_DIR layout — VERIFIED
on this machine, assume same on VM, confirm once with `echo $XDG_RUNTIME_DIR`).

## 5. Tailscale / internal-only constraints

- Tailscale is NOT installed on this machine (`which tailscale` empty) — all
  tailscale specifics are DOC-CITED (tailscale.com docs: Serve, serve CLI ref),
  UNVERIFIED on the target VM.
- `tailscale serve <local-port>` exposes a localhost-bound service **to the tailnet
  only**; `tailscale funnel` is the public-internet variant — MUST NOT use funnel
  (hard requirement: internal-only).
- Dashboard must `listen on 127.0.0.1` (or the tailscale interface) inside the
  container with host-side publish bound the same way (`127.0.0.1:PORT:PORT`);
  never `-p PORT:PORT` (binds 0.0.0.0). Best practice per tailscale docs: service
  listens on localhost, `tailscale serve` fronts it; identity headers (`Tailscale-User-Login`)
  available for auth but only trustworthy if the service is NOT directly reachable.
- Secrets: opencode auth (`~/.local/share/opencode/auth.json`) must NEVER be
  mounted into the dashboard container — mount only `opencode.db`/`storage` paths
  needed, read-only. Dashboard needs no opencode credentials for read-only
  monitoring (VERIFIED: sqlite + zellij CLI + /proc need no tokens).
- ACLs: optionally restrict Serve servicers via tailnet ACLs (doc-cited).

## 6. Architecture comparison: poll-based collector vs event/file-watch streamer

A. Poll-based collector (RECOMMENDED for v1):
- Loop every N sec: `list-sessions -s` → per live session `list-panes -j` →
  join with `/proc` liveness + sqlite `session` rows (time_updated, tokens, cost).
- Latency = poll interval (5–15 s fine for a status board). Resources trivial:
  fork/exec of zellij CLI is milliseconds; JSON parse small. Failure mode benign
  (stale frame, self-heals next tick). Stateless, easy to reason about, matches
  "plug-n-play" (no daemons, no sockets to hold open).
- Live text on demand only: `dump-screen` per focused pane, not every tick.

B. Event/file-watch streamer:
- `zellij subscribe -f json --pane-id ...` per watched pane (render deltas) +
  inotify/sqlite-wal watch on `opencode.db-wal` for message/cost updates.
- Latency sub-second, truly "live". Costs: one long-lived subscription process
  per pane (fan-out), reconnect logic on pane exit/resurrect, scrollback volume
  can be large, sqlite WAL parsing is brittle across opencode versions.
- Verdict: streamer is phase 2 for a "watch this pane live" drill-down; the
  board itself should poll. Hybrid: poll for presence/liveness + subscribe only
  for the single pane the user is viewing.

## 7. Hard cases

- opencode OUTSIDE zellij (plain ssh pane, no multiplexer): invisible to zellij
  APIs. Catch via `--pid=host` process scan (`/proc/*/cmdline` matching `opencode`,
  VERIFIED pattern works: cmdline `opencode` seen live) joined on `/proc/<pid>/cwd`
  for project. Dashboard must union zellij panes + proc-scan orphans; orphans get
  no `dump-screen` (no zellij) — show DB + proc data only.
- Multiple projects: `list-panes` CWD + opencode `session.directory`/`project_id`
  (schema VERIFIED §8) disambiguate; a pane CWD may differ from session directory
  (session opened elsewhere) — treat pane CWD as "where it runs", DB directory as
  "what it works on", show both.
- Stale/dead: EXITED zellij row + no socket entry + no server PID = dead session,
  resurrectable (re-runs command — warn user it restarts opencode, does not resume
  the process). Pane `exited:true`/`exit_status` in `-j` output = dead pane in a
  live session. `/proc` gone + DB `time_updated` old = dead orphan. `delete-session`
  / `delete-all-sessions` prune EXITED metadata (cited #4641).
- Permissions: container UID must equal host UID or mounts show as
  nobody/nogroup and sockets refuse connection. Use `user: "${UID}:${GID}"` /
  `--user $(id -u):$(id -g)`. Sockets under /run/user/<uid> are 0700 by default —
  same-user is effectively mandatory. Read-only (`:ro`) mounts for everything.

## 8. Opencode data anchor (for sibling lane; observed, not duplicated)

`~/.local/share/opencode/opencode.db` (sqlite, WAL mode): tables include
`project, session, message, part, todo, ...` (VERIFIED full list via python3
sqlite3 — `sqlite3` CLI absent on this box). `session` columns:
id, project_id, directory, title, time_created/updated, tokens_*, cost, model,
agent, ... Join key to runtime: `session.directory` ≈ pane CWD; no PID stored —
liveness comes ONLY from /proc + zellij, never from the DB.

## 9. Verification ledger

VERIFIED on this machine: zellij 0.44.3 present; live sessions + EXITED rows;
socket path + dir-membership=liveness; server PPID 1 + client attach model;
pane→opencode PPID chain; /proc State S + cwd; list-panes table/JSON incl.
COMMAND+CWD+EXITED; cross-session `--session` queries; dump-screen stdout;
query-tab-names/list-tabs/list-clients/current-tab-info; opencode.db tables +
session schema + counts; docker 29.3.1 present; tailscale ABSENT here.
DOC-CITED: session-resurrection semantics (zellij.dev); server-panic edge (#4805);
`session_serialization false` (#4641); --pid=host + bind-mount + netdata pattern
(docs.docker.com); tailscale serve vs funnel + localhost-bind practice (tailscale.com).
UNVERIFIED: subscribe streaming session; plugin pipe end-to-end; `zellij web`;
cache-dir resurrection internals; anything tailscale on the target VM; full
compose run on the VM (no VM access from here).
