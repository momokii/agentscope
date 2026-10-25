# SECURITY — opencode mission control (Wave 4, W4-2)

> Read this before `docker compose up`. Deny-by-default posture throughout:
> unset password = explicit tailnet-trust mode (documented below, not
> accidental); malformed config = refuse to start, never silently open.
> v1 auth model is **basic-auth-or-open only** — no sessions, no cookies,
> no OAuth, no user management.

## 1. Threat model

| Attacker | Can reach | Cannot reach | Why |
|---|---|---|---|
| Local attacker (other UID on the VM) | Nothing, if UID parity holds | DB, sockets, dashboard port | Sockets are `0700` (same-user mandatory); Go binds `127.0.0.1`; mounts are `:ro` into the container |
| Tailnet peer (device on your tailnet) | Dashboard URL via `tailscale serve`, gated by Basic auth when `DASHBOARD_AUTH` is set | Host filesystem, opencode DB, zellij sockets | Serve proxies HTTP only; secrets never enter the image/API/logs (§3) |
| Container escape (process breaks out of the container) | Host paths that are mounted `:ro` (read-only — observable, not writable) | `auth.json`, credential/account stores, host write access | §2 allowlist: those paths are never mounted; every mount is `:ro` |

Out of scope for v1: prompt-injection via the UI (read-only UI, no prompt
path, no shell exec from the browser), supply-chain of base images (W4-1
pins the image; `docker history` check in §5 covers secret leaks).

## 2. Mount allowlist (FULL — nothing else may be mounted)

Per the work-plan Docker spec (`.omo/plans/opencode-dashboard.md` § "Docker
plug-n-play spec"). W4-1 owns `Dockerfile`/`compose`; this list is the
review gate — any mount not on it blocks the F4 release.

| Host path | Container path | Mode | Purpose | Why nothing else |
|---|---|---|---|---|
| `/run/user/<uid>/zellij` | `/run/user/<uid>/zellij` | `:ro` | zellij CLI socket dir listing = pane liveness #1 | Presence only; no control socket writes |
| `~/.local/share/opencode/opencode.db` (+`-wal`/`-shm` siblings via dir semantics) | `/data/opencode/opencode.db` | `:ro` | Read-only WAL snapshot queries (indexed/sliced allowlist, `internal/db`) | 8.2 GB danger: full `part.data LIKE` scans banned; read-only open never disturbs live TUIs |
| `~/.local/share/opencode/storage` | `/data/opencode/storage` | `:ro` | Session file attachments referenced by DB rows | Content display only |
| `~/.cache/zellij` | `/data/zellij-cache` | `:ro` | EXITED-session semantics (resurrect re-runs, does not resume) | Dead-pane forensics only |

**Never mounted, never will be:** `~/.local/share/opencode/auth.json`
(opencode server password + provider OAuth), any `credential`/`account*`
stores. Proof commands in §5 (`docker inspect` + repo greps).

## 3. Secret inventory (where each lives + proof it never leaks)

| Secret | Lives in | Enters image? | Enters API/logs? | Proof |
|---|---|---|---|---|
| `auth.json` (opencode server password, provider tokens) | Host `~/.local/share/opencode/` only | No — never mounted (§2) | No — no code reads it; Raw tab strips credential/account payloads | `docker inspect` mount check + repo grep, §5 rows 1–2 |
| `credential` / `account*` tables (opencode.db) | Host DB file | No (file is mounted `:ro` but no query touches those tables) | No — `internal/db` query allowlist has no `credential`/`account` statement; API shapes are frozen (`SessionRow`, `KPIs`) | repo grep, §5 rows 3–4 |
| `DASHBOARD_AUTH` (`user:pass`) | Operator's env / compose `environment:` only | No — build-time env is not baked in (W4-1: no `ARG`/`ENV` for it in Dockerfile) | No — `internal/auth` has no log/slog/fmt path (test-pinned); 401 body is empty; `WWW-Authenticate` carries only the realm | `docker history` scan + unit test + grep, §5 rows 5–7 |
| Dashboard gate password (at rest) | Nowhere — compared in memory at request time, never persisted | n/a | n/a | By construction: no store, no file, no cookie |

`DASHBOARD_AUTH` format: `user:pass`, split on the FIRST colon
(passwords may contain colons). Empty/unset = open tailnet-trust mode
(explicit, see §4). No colon, empty user, or empty password → startup
fails closed with `*auth.AuthConfigError` (matchable via
`errors.Is(err, auth.ErrInvalidAuthConfig)`); the process refuses to
serve. Auth is read from env **only** — never from URL query params,
request bodies, or any other input.

## 4. Network posture (127.0.0.1-only × 3 layers)

1. **Go bind:** `ports.Config.BindAddr` is hardcoded `"127.0.0.1"` — no env
   override by design. `hub.Server.Run` listens on `cfg.Addr()` only.
   `grep -rn '0\.0\.0\.0' --include='*.go' internal/` must show only
   comments/tests asserting its absence.
2. **No published ports to misconfigure:** compose sets
   `network_mode: "host"` and carries NO `ports:` stanza (ignored under
   host mode — its absence is the review gate). The 127.0.0.1 bind lands
   on host loopback with no NAT hop, so there is no publish line that
   could ever read `0.0.0.0`. Proof: `docker compose config` shows
   `network_mode: host` and no `ports:`; `ss -tlnp | grep $PORT` shows
   the dashboard binary itself on `127.0.0.1:$PORT` with no
   `docker-proxy` line.
3. **Tailnet exposure:** `tailscale serve` fronts localhost. **`funnel`
   (public internet) is banned.** Rationale: Basic auth is base64
   (reversible, not encryption) and tailnet identity/ACLs are the actual
   perimeter; funnel would put a reversible-credential gate on the open
   internet with no additional control.

Copy-paste serve command (run on the VM, after `curl` health check):

```bash
curl -s 127.0.0.1:$PORT/api/healthz
# want: {"healthy":true,...}

tailscale serve --bg --https=443 http://127.0.0.1:$PORT
tailscale serve status   # confirm: serve on, funnel off
```

Tailnet ACL note (optional allowlist — restrict the dashboard to tagged
owners/admins; adjust to your tailnet):

```json
{
  "acls": [
    {"action": "accept", "src": ["tag:owner", "tag:admin"], "dst": ["tag:dashboard:443"]}
  ]
}
```

With `DASHBOARD_AUTH` set, a tailnet peer still passes the browser Basic
prompt (§6) — ACLs and the gate are independent layers; either may be
the tighter one.

Deliberate trade (stated, not hidden): the container shares the host
network namespace. This extends the already-accepted `pid: "host"` pattern
(research-runtime-observability §4) for a monitoring agent that already
joins host PID and `:ro` host mounts — the blast-radius delta is nil
beyond what those grants imply, and it removes the NAT hop that made the
service unreachable.

Rejected alternative (recorded, do NOT implement): binding `0.0.0.0`
inside a bridge-netns container. It would weaken the in-code
`assertLoopback` invariant (`cmd/dashboard/selfcheck.go`) and require
`BIND_ADDR`-style env-override machinery the codebase deliberately bans
(no env override by design); a future refactor could then silently expose
the dashboard. Host mode keeps every loopback guarantee intact with zero
code changes.

Opt-in non-loopback bind (guarded, tailnet-only): `BIND_ADDR` (default
`127.0.0.1`, unset = posture unchanged) may be set to e.g. `0.0.0.0` to
reach the dashboard directly at `http://<tailnet-ip>:$PORT` without
`tailscale serve`, but ONLY with `DASHBOARD_AUTH=user:pass` set — any
non-loopback bind without valid auth makes the binary refuse to start
(fail closed, no secret in the FATAL text). Tailnet-only assumption holds:
never use this on a publicly reachable host (full transcripts are readable
by anyone who can reach the port); `funnel` stays banned.

## 5. Security Gate checklist (reviewer runs verbatim)

Repo greps (runnable now — pasted outputs verified 2026-10-24):

```bash
# 1. No auth.json reference in code/image descriptors (expect: exit 1, no output)
grep -rni 'auth\.json' --include='*.go' --include='*.ts' --include='*.tsx' \
  --include='Dockerfile*' --include='*.yml' --include='*.yaml' \
  internal/ web/src/ .env.example
# EXPECTED: (no output) exit 1
# ACTUAL (2026-10-24): no output, exit 1 — clean

# 2. No credential/account table access (expect: only internal/auth gate-credential hits)
grep -rni 'credential' internal/ web/src | grep -v internal/auth
# EXPECTED: (no output) exit 1
# ACTUAL (2026-10-24): no output outside internal/auth — clean
grep -rni 'account' --include='*.go' internal/db/ internal/collector/ internal/hub/
# EXPECTED: (no output) exit 1
# ACTUAL (2026-10-24): no output, exit 1 — clean

# 3. No 0.0.0.0 listener (expect: only comments/tests asserting absence)
grep -rn '0\.0\.0\.0' --include='*.go' internal/
# EXPECTED: hits in comments/tests only (ports.go, handlers.go comments, bind-absence test)
# ACTUAL (2026-10-24): 4 hits, all comments/test — clean

# 4. Frontend sends no auth headers of its own (expect: exit 1, no output)
grep -rni 'authorization\|setbasic\|withcredentials' web/src/
# EXPECTED: (no output) exit 1 — sse.ts untouched by design, see §6
# ACTUAL (2026-10-24): no output, exit 1 — clean

# 5. Auth package has no logging path (expect: green)
go test -count=1 ./internal/auth/ -v
# EXPECTED: 15/15 pass incl. Test_Package_emits_no_logs + Test_Timing_compare_uses_constant_time
# ACTUAL (2026-10-24): 15 passed — green
```

Image/host checks — TO-RUN after W4-1's image exists (stated boundary:
these cannot run until W4-1 lands `Dockerfile` + `compose`):

```bash
# 6. Mount allowlist: exact output must equal §2 (no auth.json, all :ro)
docker inspect <container> --format '{{json .Mounts}}' | python3 -m json.tool
# EXPECTED: only the 4 §2 mounts, every one "Mode": "ro" / "RW": false

# 7. No secret baked into image layers
docker history --no-trunc <image> | grep -i 'auth\|pass\|secret\|token\|credential'
# EXPECTED: (no output) exit 1

# 8. Loopback-only listen, no publish to misconfigure (host mode)
docker compose config | grep -E 'network_mode|ports:'
# EXPECTED: `network_mode: host` — and NO `ports:` line (grep finds only the network_mode hit)
ss -tlnp | grep $PORT
# EXPECTED: one 127.0.0.1:$PORT line owned by the dashboard binary, NO docker-proxy line, NO 0.0.0.0:$PORT line
curl -s --max-time 5 http://127.0.0.1:$PORT/api/healthz
# EXPECTED: {"healthy":true,...}
```

## 6. SSE + Basic auth interaction (investigated, with citations)

**Finding: no `web/src` change needed — same-origin Basic just works, with
one ugly edge documented below.**

- Native `EventSource` cannot set custom headers: the constructor takes
  only `(url, {withCredentials})` — there is no header slot. (MDN
  [`EventSource`](https://developer.mozilla.org/en-US/docs/Web/API/EventSource);
  WHATWG HTML § server-sent events; corroborated by
  [fetch-event-source "SSE Auth" writeup](https://milanjovanich.tech/blog/fetch-event-source)
  and long-standing
  [Stack Overflow consensus](https://stackoverflow.com/questions/28176933/http-authorization-header-in-eventsource-server-sent-events).)
  Credential-in-URL (`https://user:pass@host/`) is dead — modern browsers
  strip it before sending
  ([MDN HTTP Authentication](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Authentication#access_using_credentials_in_the_url)).
- But we do not need headers: this dashboard's SPA is served from the same
  Go binary as the API (same origin), and Basic follows the standard HTTP
  challenge flow — server `401` + `WWW-Authenticate`, client presents
  `Authorization`, browser prompts once per session and caches per origin
  ([MDN HTTP Authentication framework](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Authentication#the_general_http_authentication_framework)).
  After that one prompt, the browser's HTTP layer attaches the cached
  credential to **every** same-origin request automatically — including
  `new EventSource("/api/events")` (plain same-origin GET, no CORS
  preflight: the hub emits no CORS headers by design) and the `fetch()`es
  in `sse.ts` (default `credentials: same-origin`). The
  `?lastEventId=` resume-query fallback carries only the numeric cursor,
  never a secret.
- **The ugly edge (stated, not hand-waved):** if the operator cancels /
  fails the browser prompt, `EventSource` receives `401`s, fires `error`
  events, and our client treats them as drops — it re-syncs via `fetch`
  (also 401) and reconnects forever with backoff, showing "reconnecting…".
  That is a loud, non-silent failure (good), but it is a hot 401 loop, not
  a login form. Follow-up if it ever hurts: replace Basic with a
  cookie-session login (fetch-based `Authorization` is possible via the
  `eventsource` npm package's `fetch` override or `@microsoft/fetch-event-source`);
  explicitly deferred — v1 stays basic-auth-or-open.

## 7. W4-1 wiring contract (main.go — owned by W4-1, do not duplicate)

`internal/hub.Server.Use` is the only seam. W4-1's `cmd/` startup applies
the gate with exactly these lines (fail closed on malformed config):

```go
creds, err := auth.CredentialsFromEnv()
if err != nil {
    return fmt.Errorf("dashboard auth: %w", err) // refuse to serve
}
if creds.Enabled {
    srv.Use(func(next http.Handler) http.Handler {
        return auth.Middleware(next, creds.Username, creds.Password)
    })
}
```

Test hook: after `Use`, `Server.Handler()` returns the full chain
(middleware included), so `httptest` covers the gate.
