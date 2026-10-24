# Research: opencode session data sources (Lane A — opencode internals)

Date: 2026-10-24. Researcher: subagent (Lane A). Machine: kelanach-ThinkPad-E14-Gen-3, opencode 1.18.35.
Scope: ONLY opencode-the-harness (SST/anomalyco opencode TUI+server CLI). NOT OpenAI Codex, NOT Claude Code.

## 0. Tool identity (verified)

- Binary: `/home/kelanach/.opencode/bin/opencode`, ELF 64-bit, ~185MB single-file executable, `opencode --version` = **1.18.35**.
- Help banner shows `opencode [project]` TUI default + subcommands: `acp, mcp, attach, run, debug, providers(auth), agent, upgrade, uninstall, serve, web, models, stats, export, import, github, pr, session, plugin, db`. Source: `opencode --help` observed on this machine.
- Upstream is now `anomalyco/opencode` (docs at opencode.ai/docs, repo moved from sst/opencode). Docs fetched 2026-10-24: `/docs/server/`, `/docs/web/`, `/docs/cli/`, `/docs/sdk/`.
- The `bg_*` background-task ids (e.g. `bg_1b5f8585`) seen in sibling prompts are **THIS harness's** task handles, NOT opencode ids. Opencode's own id prefixes: `ses_*` sessions, `msg_*` messages, `prt_*` parts, `call_*`/`call_function_*` tool calls. Do not conflate.

## 1. Storage map (all verified by direct inspection)

| Path | What | Format / notes |
|---|---|---|
| `~/.local/share/opencode/opencode.db` (+ `-wal`, `-shm`) | ALL sessions, messages, parts, todos, events, projects | SQLite, WAL mode, **8.2 GB** on this box. Single writer set: every live `opencode` TUI holds fds to db+wal+shm (verified via /proc/<pid>/fd). Dashboard can open read-only; prefer `opencode db "<sql>"` or SQLite WAL read-only snapshot. |
| `~/.local/share/opencode/auth.json` | provider credentials | JSON (2.4K). SENSITIVE — dashboard must never expose. |
| `~/.local/share/opencode/log/` | `opencode.log` (114MB, live) + rotated `YYYY-MM-DDTHHMMSS.log` | Line-oriented `timestamp=... level=... service=... session.id=ses_...` — greppable live stream. Contains `session.id`, `messageID`, `run=<id>`, `agent=... mode=subagent`, `message=loop/step/stream/process`, permission evaluations. |
| `~/.local/share/opencode/storage/session_diff/ses_*.json` | per-session file diffs | JSON arrays (sample was `[]`). |
| `~/.local/share/opencode/storage/{agent-usage-reminder,directory-readme,migration,oh-my-openagent,rules-injector}` | plugin/feature storage | `oh-my-openagent/` holds `tui-state`. |
| `~/.local/share/opencode/snapshot/<projectID>/<hash>/` | bare **git repos** (branches, HEAD, index, objects/) | File snapshots backing revert/diff (`session.revert`, `summary_diffs`). `<projectID>` matches `project.id`. |
| `~/.local/share/opencode/tool-output/tool_*` | large tool outputs spilled to disk | Referenced from tool part metadata (`truncated:true`). My own `strings` dump landed here — harness spills big bash outputs here. |
| `~/.local/share/opencode/repos/` | empty on this machine | (Purpose UNVERIFIED — likely worktree/repo cache.) |
| `~/.config/opencode/` | global config: `opencode.json`, `tui.json`, `lsp-install-decisions.json`, `.omo/`, `.codegraph@`, `node_modules/`, `oh-my-openagent/` plugin | Project used global `opencode.json` (`{"model":"opencode-go/muse-spark-1.3-contributor"}`). |
| `~/.local/state/opencode/` | TUI state: `session.json` (`{"pinned":[]}`), `kv.json` (theme/sidebar/thinking prefs), `frecency.jsonl`, `model.json`, `prompt-history.jsonl` (152K), `plugin-meta.json`, `locks/` (empty) | No live session status here. |
| `~/.cache/opencode/` | `models.json` (5.9MB), `packages/` (plugins incl. oh-my-openagent), `skills/`, `bin/` | Plugin code; `models.json` = models.dev cache. |
| `/tmp/opencode` | `opencode debug paths` lists as `tmp` (`state` = `~/.local/state/opencode`) | Not inspected. |
| Project-local `.opencode/` | ABSENT in opencode-dashboard project (`ls` → No such file) | State is global, not per-project. Do not assume project dirs exist. |

Global paths verified via `opencode debug paths` (data/bin/log/repos/cache/config/state/tmp above) and `opencode db path` → `~/.local/share/opencode/opencode.db`.

## 2. SQLite schema (verified: `opencode db "SELECT name, sql FROM sqlite_master WHERE type='table'"`)

Tables: `__drizzle_migrations, project, message, part, session, todo, session_share, control_account, account, account_state, event_sequence, event, workspace, session_message, data_migration, migration, permission, session_input, session_context_epoch, credential, project_directory`.

### `session` columns (ENUMERATE EVERYTHING — dashboard field list)
`id (ses_*)`, `project_id` (FK; often literal `"global"`), `parent_id` (NULL = top-level; else SUBAGENT child), `slug` (e.g. `kind-sailor`), `directory` (cwd), `path`, `title`, `agent` (e.g. `Atlas - Plan Executor`, `general`, `Sisyphus-Junior`), `model` (JSON: `{id, providerID, variant}` e.g. opencode-go/muse-spark-1.3-contributor), `version` (binary version), `share_url`, `summary_additions/deletions/files`, `summary_diffs`, `revert`, `permission`, `cost` (REAL USD), `tokens_input/output/reasoning/cache_read/cache_write` (INTs), `metadata`, `workspace_id`, `time_created/updated/compacting/archived` (ms epoch; compacting/archived nullable).
- `session list --format json` (CLI) returns a SUBSET: id/title/updated/created/projectId/directory only. Full row needs `opencode db` or `/session/:id`.
- `opencode export <sesID>` returns `{info:{...full session incl. tokens{input,output,reasoning,cache{read,write}}, time{created,updated}, summary, share?}, messages:[{info, parts[]}]}` — best full-context dump (verified, sample captured).

### `message` columns: `id (msg_*), session_id, time_created, time_updated, data(JSON)`.
- User msg data: `{role:user, time:{created}, agent, model:{providerID,modelID}, system?, tools?, format?, summary?, id, sessionID}` + parts.
- Assistant msg data: `{role:assistant, parentID, mode, agent, path:{cwd,root}, modelID, providerID, variant?, cost, tokens{total?,input,output,reasoning,cache{read,write}}, time:{created, completed?}, finish?, error?, summary?}`. **`time.completed` NULL = STILL GENERATING** (verified: in-flight msg_16d540245001... has completed:null while siblings have timestamps). `finish` ∈ observed `stop`, `tool-calls`. `error` ∈ SDK union: ProviderAuthError|UnknownError|MessageOutputLengthError|MessageAbortedError|StructuredOutputError|ContextOverflowError|ContentFilterError|APIError.

### `part` columns: `id (prt_*), message_id, session_id, time_created, time_updated, data(JSON)`.
Part `type` enum (verified against live SDK types @opencode-ai/sdk 1.18.x + local samples): `text{+synthetic?,ignored?,time{start,end?}}`, `reasoning{text,time,metadata}`, `tool{callID,tool,state}`, `step-start{snapshot?}`, `step-finish{reason,tokens,cost}`, `file{mime,filename?,url,source?}`, `subtask{prompt,description,agent,model?,command?}`, `snapshot`, `patch{hash,files}`, `agent{name}`, `retry{attempt,error,time}`, `compaction{auto,overflow?}`.
- Tool `state.status` ∈ `pending|running|completed|error`; running has `time{start}` only; completed has `time{start,end,compacted?}`, `title`, `output`, `metadata{filepath?,exit?,truncated?,...}`; observed tools: bash/read/edit/todowrite/glob/task.
- Subagent spawn = `tool:"task"` part with `state.input{description, prompt, ...}` (verified 3 task parts in parent ses_e92b7bc3...: "Research opencode session internals" etc.) + child session row. NO `subtask`-type parts found in parent (scoped query returned []).

### `todo`: PK(session_id,position); `content,status{pending,in_progress,completed,cancelled},priority{high,medium,low},time_created/updated`. Verified samples.
### `event` / `event_sequence`: THE LIVE BUS (persisted). `event{id,aggregate_id(=sessionID),seq,type,data}`; types observed in DB (suffix `.1` = versioned): `message.part.updated.1, message.updated.1, session.created.1, session.updated.1, message.removed.1`. `event_sequence{aggregate_id,seq,owner_id}` = per-session cursor. Latest rows at any moment show in-flight tool parts (`state.status:running` — INCLUDING the dashboard's own probe queries, self-observing). Order by `rowid DESC` = real-time firehose. (Newer SDK documents `session.next.*` streaming events + `session.status/session.idle` — not observed in this DB's `event.type` set; treat as UNVERIFIED for this version's persistence.)
### `session_message{session_id,type,time_created/updated,data,seq}` and `session_input{id,session_id,prompt,delivery{steer|queue?},admitted_seq,promoted_seq?,time_created}`: both EMPTY on this machine right now — they are the prompt-queue path (`prompt_async`, steer/queue delivery). Empty = nothing queued.
### `project{id,worktree,name,icon_*,vcs,time_*,sandboxes,commands}`: `global` worktree=`/` + one row per project dir hash. `workspace{type,name,branch,directory,...}`, `project_directory`, `session_share`, `session_context_epoch`, `permission` (= permission RULES per project, not pending requests), `credential/account*` (SENSITIVE).

## 3. Subagent visibility (verified)

- Child sessions: `session.parent_id = <parent ses_*>`; title convention `"... (@<agent> subagent)"` (observed: `(@general subagent)`, `(@Sisyphus-Junior subagent)`); `agent`/`model` columns name the subagent + its model. Verified 2 children of ses_e92b7bc3... + 1 child elsewhere.
- API: `GET /session/:id/children` (documented). CLI has no children flag — use SQL `WHERE parent_id=?` or the API.
- In parent transcript: `tool:"task"` parts (description/prompt in input; link to child via timing/title, no explicit child-session FK in the part — CORRELATE via parent_id + description. Mark as heuristic, not FK.)
- `session.next.*` events (documented in SDK GlobalEvent) include step/text/tool/reasoning deltas per session — the streaming layer for live tool-call display.

## 4. Status semantics: running vs idle vs waiting vs done (verified where stated)

- SDK `SessionStatus` (zod `session/status.ts`, SDK v1+v2): `{type:idle} | {type:retry, attempt, message, next(, action?)} | {type:busy}`. There is NO persisted status column — status is IN-MEMORY per server instance.
- `GET /session/status` → `{sessionID: SessionStatus}` **for that server instance only**. VERIFIED live: fresh `opencode serve --port 4096` returned `{}` (nothing running on it) while 3 other TUI sessions were busy — statuses are per-process, not global. Dashboard MUST query every instance or derive from DB.
- DB-derived signals (verified queries):
  - RUNNING: `message` with `role=assistant AND time.completed IS NULL` (verified live), OR `part` tool state `running`, OR fresh `event` rows (`message.part.updated.1`) for the session (verified: my own probe appeared instantly).
  - IDLE/DONE: latest assistant message has `time.completed` set; `session.time_updated` stale; no running parts.
  - WAITING-FOR-USER (heuristic, no explicit flag found): latest message role=user with no in-flight assistant, OR `time_compacting` set (compacting), OR `time_archived` set. No `question`/`permission_request` table exists in this schema — pending interactive prompts appear in-memory only (UNVERIFIED persistence; opencode-book describes a `pending` state for permission/question, but the zod union has only idle/retry/busy — treat `pending` as UNVERIFIED for v1.18).
  - Heartbeats: `session.time_updated` (ms) bumps on every event; `opencode.db-wal` mtime + `log/opencode.log` tail (`message=loop ... step=N`, `message=stream ... session.id=...`, `message=process ... messageID=...`) are the cheapest poll-free-ish signals (inotify on WAL + log).
- `POST /session/:id/abort` exists (documented) to stop a running session.

## 5. HTTP API surface (verified live on this machine + official docs)

- `opencode serve [--port N] [--hostname H] [--cors ...]` → headless REST server. VERIFIED: `opencode serve --port 4096` printed `opencode server listening on http://127.0.0.1:4096`; `GET /global/health` → `{"healthy":true,"version":"1.18.35"}`; `GET /session` → full session JSON array; `GET /session/status` → `{}`. Killed after probe (`opencode serve` process ends; TUI sessions unaffected).
- Auth: `OPENCODE_SERVER_PASSWORD` (+ optional `OPENCODE_SERVER_USERNAME`, default `opencode`) → HTTP basic auth. Applies to serve AND web. (Docs; password unset here.)
- Full route list (source: opencode.ai/docs/server, consistent with sst/anomalyco `server.ts` source): global(`/global/health`, `/global/event` SSE), project(`/project`, `/project/current`), `/path`, `/vcs`, `/instance/dispose`, config(`/config`, PATCH, `/config/providers`), provider(`/provider`, `/provider/auth`, `/provider/{id}/oauth/*`), sessions(`GET/POST /session`, `/session/status`, `GET/DELETE/PATCH /session/:id`, `/children`, `/todo`, `/init`, `/fork`, `/abort`, `/diff`, `/summarize`, `/revert`, `/unrevert`, `/permissions/:permissionID`), messages(`GET/POST /session/:id/message`, `GET .../message/:messageID`, `/prompt_async`, `/command`, `/shell`), `/command`, files(`/find`, `/find/file`, `/find/symbol`, `/file`, `/file/content`, `/file/status`), experimental(`/experimental/tool/ids`, `/experimental/tool`), status(`/lsp`, `/formatter`, `/mcp`, `POST /mcp`), `/agent`, `POST /log`, TUI control(`POST /tui/*`, `GET /tui/control/next`, `POST /tui/control/response`), auth(`PUT/DELETE /auth/:providerID`), events(`GET /event` SSE; first event `server.connected`), docs(`GET /doc` OpenAPI 3.1 HTML).
- `opencode web [--port]` = serve + browser UI (sessions list, active sessions, "See Servers", attach terminal via `opencode attach <url>`). Note issue #2462: base URL returns 404 by design (API-only; `/doc` is the human entrypoint).
- `opencode attach <url> [--dir] [-s session]` drives a remote server's TUI. `opencode run [message] [--session] [--format json] [--attach <url>]` = headless prompt runner.
- TUI architecture (corroborated by sst/opencode#2245 debug logs): `opencode` process = server; TUI client talks to it over HTTP (`GET /agent`, `/project/current`, `/path`, `/config`, `/command`, `/file/status`, `GET /event` SSE + `server event connected`, `GET /tui/control/next` long-poll).

## 6. UNVERIFIED / anomalies (do not build on without re-check)

- U1: Every live TUI `opencode` process holds TWO 127.0.0.1 listeners (e.g. pid 3850783: 39107+40899; pattern holds for all 5 processes; fd31/fd32 since startup; DB fds are 24-26). Probing `GET /session|/global/health|/doc|/project|/session/status|/event|/agent|/tui/control/next` on them returns EITHER plaintext `not found` (one port) OR `{"error":{"type":"not_found","message":"Endpoint not supported: GET <path>"}}` (other port, HTTP 404, CORS `*`, no Server header). The string "Endpoint not supported" is NOT in the opencode binary (full `strings` scan) nor in oh-my-openagent packages. Owner verified via /proc/net/tcp inode↔fd mapping (40899=0x9FC3 inode 16853446 = fd32 of pid 3850783). So the JSON-error server is bound to opencode's socket but serves unknown protocol — possibly a plugin in-process server or a version-skewed internal API. NEEDS follow-up (check `OPENCODE_SERVER_*` env under serve, plugin hook servers, `ss -xp` socket options).
- U2: `pending` (waiting-permission) session state — described by opencode-book (4.6) as `idle→busy→idle + pending`, but the shipped zod `SessionStatus` union is idle|retry|busy only. No DB table holds pending permission/question requests. Liveness for "waiting-for-user" must be heuristic until verified against a live permission prompt (watch `/question`, `/permission` routes + `session.next.*` events).
- U3: `session_message`/`session_input` were empty — queue/steer semantics verified only from SDK type names + docs (`prompt_async`), not live rows.
- U4: `repos/` dir empty; `storage/{agent-usage-reminder,directory-readme,migration,rules-injector}` contents not read; `session.path` vs `directory` distinction not nailed down.
- U5: `event` table shows DUPLICATE consecutive rows for the same part update (observed 2 identical `message.part.updated.1` rows) — dedupe by (id) or (aggregate_id,seq) when tailing.

## 7. CLI quick reference (all observed on this machine)

- `opencode session list [--format table|json] [-n N]` — table default; json gives id/title/updated/created/projectId/directory (ms epochs).
- `opencode session delete <sessionID>`; `opencode export [sessionID] [--sanitize]` (full JSON dump); `opencode import <file|URL>`.
- `opencode stats [--days N] [--tools N] [--models N] [--project X]` — VERIFIED sample: OVERVIEW(Sessions/Messages/Days), COST & TOKENS(total/avg/input/output/cache), TOOL USAGE bars (bash 35.5%, read 24.2%, ...).
- `opencode db [query] [--format json|tsv]` + `opencode db path`. Queries hit the LIVE 8.2GB WAL db — keep them indexed (session_id/message filters) or they time out (full `part.data LIKE` scan >60s).
- `opencode agent list` (verbose permission dumps), `opencode agent create`; `opencode debug {paths,info,config,scrap,skill,lsp,snapshot,startup,agent,v2}`; `opencode models [provider]`; `opencode --port/--hostname/--mdns` also apply to bare `opencode` TUI (each TUI gets random port by default).

## 8. Recommended dashboard data strategy (research conclusion, not implementation)

1. Primary source = `opencode.db` (SQLite WAL, read-only): sessions+messages+parts+todos+events give 100% of content, cost/tokens, parent/child, in-flight detection (`completed IS NULL`, running tool states), full transcript. Poll `MAX(time_updated)` + `MAX(event.rowid)` or inotify the `-wal` file.
2. Real-time = tail `event` table by rowid (dedupe!) and/or `log/opencode.log` (session.id-tagged stream/loop/step lines); per-session SSE via `opencode serve` on a FIXED port (`GET /event`, `/global/event`) as the push channel.
3. Presence = `pgrep -a opencode` + `ss -tlnp` (which dirs are live) joined to `session.directory`; `/session/status` only covers the queried server instance — run ONE `serve` bound to 127.0.0.1 as the dashboard's own API gateway, or iterate instances.
4. Never expose `auth.json`, `credential/account*` tables, or `OPENCODE_SERVER_PASSWORD`. Serve API on 127.0.0.1 only (or Tailscale-serve with password) given the VM/docker plan.
5. Watch out: 8.2GB DB — dashboard queries MUST be indexed/sliced (per-session, LIMIT) ; `export` is per-session and safe.

## 9. Sources

- PRIMARY (this machine): `opencode --help`, `session --help`, `serve/web/db/export/attach/run/agent/stats` helps; `opencode debug paths`; `opencode session list --format json`; `opencode export ses_e92b7bc3...`; `opencode stats --days 1`; `opencode db` schema dump + ~15 scoped row queries; /proc fd+net/tcp mapping; ss/pgrep; direct file reads (state/storage/snapshot/log/auth.json sizes).
- DOCS: https://opencode.ai/docs/server/ (routes, auth, attach, SDK note), /docs/web/ (web UI, servers list, config-file server block), /docs/cli/, /docs/sdk/ (types import).
- CODE: anomalyco/opencode `packages/opencode/src/server/server.ts` (route tree), `.../server/routes/instance/httpapi/errors.ts` (SessionBusyError 409 etc.), `packages/sdk/js/src/v2/gen/types.gen.ts` + jsdelivr `@opencode-ai/sdk@1.18.16` d.ts (SessionStatus, Session, Message, Part, Todo, Pty, GlobalEvent unions — status/retry/busy, tool pending/running/completed/error, subtask/agent/retry/compaction parts), sst/opencode#2245 (TUI↔server HTTP+SSE+control/next sequence), #2462 (API-only base URL), PR #5136 (endpoint inventory).
