# SECURITY — opencode mission control

> Read this before `docker compose up`. Deny-by-default posture throughout:
> unset password = explicit tailnet-trust mode (documented below, not
> accidental); malformed config = refuse to start, never silently open.
> Auth model: **Basic-auth-or-open**, with a bounded HttpOnly session cookie
> (`ocd_session`) issued after a successful Basic check so `EventSource` can
> authenticate. There is no per-user management, OAuth, or persistent session
> store.

## 1. Threat model

| Attacker | Can reach | Cannot reach | Why |
|---|---|---|---|
| Local attacker (other UID on the VM) | Nothing, if UID parity holds | DB, sockets, dashboard port | Sockets are `0700` (same-user mandatory); Go binds `127.0.0.1` unless a non-loopback `BIND_ADDR` is explicitly set with valid auth; mounts are `:ro` into the container (socket dir `:rw`, see §2) |
| Tailnet peer (device on your tailnet) | Dashboard URL via `tailscale serve`, gated by Basic auth when `DASHBOARD_AUTH` is set | Host filesystem, opencode DB, zellij sockets | Serve proxies HTTP only; secrets never enter the image/API/logs (§3) |
| Container escape (process breaks out of the container) | Host paths that are mounted `:ro` (read-only — observable, not writable), plus the socket dir (`:rw` — client sockets only, no server state worth corrupting) | `auth.json`, credential/account stores, host write access | §2 allowlist: those paths are never mounted; data/cache mounts are `:ro` |

Out of scope for v1: prompt-injection via the UI (read-only UI, no prompt
path, no shell exec from the browser), supply-chain of base images (W4-1
pins the image; `docker history` check in §5 covers secret leaks).

## 2. Mount allowlist (FULL — nothing else may be mounted)

Per the work-plan Docker spec (`.omo/plans/opencode-dashboard.md` § "Docker
plug-n-play spec"). W4-1 owns `Dockerfile`/`compose`; this list is the
review gate — any mount not on it blocks the F4 release.

| Host path | Container path | Mode | Purpose | Why nothing else |
|---|---|---|---|---|
| `/run/user/<uid>/zellij` | `/run/user/<uid>/zellij` | `:rw` (REQUIRED — the zellij CLI mints a per-invocation client socket here; `:ro` kills every pane query with EROFS) | zellij CLI socket dir listing = pane liveness #1 | Read-only queries only (`list-sessions`/`list-panes`/`dump-screen` — no attach/kill/resurrect); same-UID parity already mandatory |
| `~/.local/share/opencode` (parent dir: db + `-wal`/`-shm` + storage) | `/data/opencode` | `:ro` | Read-only WAL snapshot queries (indexed/sliced allowlist, `internal/db`); service active file attachments | Parent-dir mount keeps WAL sidecars visible; 8.2 GB danger: full `part.data LIKE` scans banned; read-only open never disturbs live TUIs |
| `~/.cache/zellij` | `/data/zellij-cache` | `:ro` | EXITED-session semantics (resurrect re-runs, does not resume) | Dead-pane forensics only |
| `~/.pi/agent/sessions` | `/data/pi-sessions` | `:ro` | Pi JSONL session store (Wave 5 second harness: status/tokens/cost from message usage) | Mounts ONLY the sessions subdir — `~/.pi/agent/auth.json` (0600 provider OAuth) and the store root never enter |
| `/etc/localtime` | `/etc/localtime` | `:ro` | Local-midnight attribution for `cost_today` | Timezone data only |

**Never mounted, never will be:** `~/.local/share/opencode/auth.json`
(opencode server password + provider OAuth), `~/.pi/agent/auth.json`
(pi provider OAuth — only `sessions/` is mounted), any `credential`/`account*`
stores. Proof commands in §5 (`docker inspect` + repo greps).

## 3. Secret inventory (where each lives + proof it never leaks)

| Secret | Lives in | Enters image? | Enters API/logs? | Proof |
|---|---|---|---|---|
| `auth.json` (opencode server password, provider tokens) | Host `~/.local/share/opencode/` only | No — never mounted (§2) | No — no code reads it; Raw tab strips credential/account payloads | `docker inspect` mount check + repo grep, §5 rows 1–2 |
| `auth.json` (pi provider OAuth, 0600) | Host `~/.pi/agent/` only | No — only `sessions/` is mounted (§2); the Go reader opens `*.jsonl` exclusively | No — no code reads it (suffix allowlist, negative-tested) | `docker inspect` mount check + repo grep, §5 rows 1–2 |
| `credential` / `account*` tables (opencode.db) | Host DB file | No (file is mounted `:ro` but no query touches those tables) | No — `internal/db` query allowlist has no `credential`/`account` statement; API shapes are frozen (`SessionRow`, `KPIs`) | repo grep, §5 rows 3–4 |
| `DASHBOARD_AUTH` (`user:pass`) | Operator's env / compose `environment:` only | No — build-time env is not baked in (W4-1: no `ARG`/`ENV` for it in Dockerfile) | No — `internal/auth` has no log/slog/fmt path (test-pinned); 401 body is empty; `WWW-Authenticate` carries only the realm | `docker history` scan + unit test + grep, §5 rows 5–7 |
| Dashboard gate password (at rest) | Nowhere — compared in memory at request time, never persisted | n/a | n/a | By construction: no file/database store. A successful Basic check mints an `ocd_session` **token** (32 random bytes, HttpOnly, 12h TTL, bounded 32-entry in-memory store) — the token is not the password and carries no user identity |

`DASHBOARD_AUTH` format: `user:pass`, split on the FIRST colon
(passwords may contain colons). Empty/unset = open tailnet-trust mode
(explicit, see §4). No colon, empty user, or empty password → startup
fails closed with `*auth.AuthConfigError` (matchable via
`errors.Is(err, auth.ErrInvalidAuthConfig)`); the process refuses to
serve. Auth is read from env **only** — never from URL query params,
request bodies, or any other input.

## 4. Network posture (loopback by default, opt-in guarded)

1. **Go bind:** `ports.Config.BindAddr` defaults to `"127.0.0.1"`. A non-loopback
   value (e.g. `0.0.0.0`) is an explicit operator opt-in enforced at startup in
   `cmd/dashboard/selfcheck.go`: the binary refuses to serve unless
   `DASHBOARD_AUTH` is valid, and never echoes the credential in the FATAL text.
   `hub.Server.Run` listens on `cfg.Addr()` only. `grep -rn '0\.0\.0\.0'
   --include='*.go' internal/` must show only comments/tests (the opt-in guard
   lives in `cmd/`).
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
joins host PID and host mounts (`:ro` data/cache, `:rw` socket dir) — the blast-radius delta is nil
beyond what those grants imply, and it removes the NAT hop that made the
service unreachable.

Rejected alternative (recorded, do NOT implement): publishing the dashboard by
binding `0.0.0.0` inside a bridge-netns container without a gate. It weakens
the `assertLoopback` invariant (`cmd/dashboard/selfcheck.go`) and creates a
publish path that can silently expose the dashboard. The shipped compromise is
host mode + a guarded `BIND_ADDR` opt-in (below): non-loopback is possible but
fails closed without valid `DASHBOARD_AUTH`.

Opt-in non-loopback bind (guarded, tailnet-only): `BIND_ADDR` (default
`127.0.0.1`, unset = posture unchanged) may be set to e.g. `0.0.0.0` to
reach the dashboard directly at `http://<tailnet-ip>:$PORT` without
`tailscale serve`, but ONLY with `DASHBOARD_AUTH=user:pass` set — any
non-loopback bind without valid auth makes the binary refuse to start
(fail closed, no secret in the FATAL text). Tailnet-only assumption holds:
never use this on a publicly reachable host (full transcripts are readable
by anyone who can reach the port); `funnel` stays banned.

## 5. Security Gate checklist (reviewer runs verbatim)

Repo greps (runnable now — outputs re-verified 2026-10-27):

```bash
# 1. No auth.json reference in code/image descriptors (expect: exit 1, no output)
grep -rni 'auth\.json' --include='*.go' --include='*.ts' --include='*.tsx' \
  --include='Dockerfile*' --include='*.yml' --include='*.yaml' \
  internal/ web/src/ .env.example
# EXPECTED: (no output) exit 1
# ACTUAL (2026-10-27): no output, exit 1 — clean
# ACTUAL (Wave 5, 2026-10-27): no output, exit 1 — clean. The pi negative
# test names the credential file only via string concatenation
# (internal/pi/pi_test.go) so the literal grep stays empty.

# 2. No credential/account table access (expect: only auth-wiring comments
#    plus the fetch API cookie option — never credential VALUES)
grep -rni 'credential' internal/ web/src | grep -v internal/auth
# EXPECTED: one comment in internal/hub/handlers.go (auth wiring snippet)
# plus `credentials: "same-origin"` fetch options (same-origin cookies,
# no values) — no table access, no secrets
# ACTUAL (2026-10-27): handlers.go comment only (fetch hits pre-date the note)
# ACTUAL (Wave 5, 2026-10-27): handlers.go comment + 5 pre-existing
# `credentials: "same-origin"` fetch/test hits — no values, no table access
grep -rni 'account' --include='*.go' internal/db/ internal/collector/ internal/hub/
# EXPECTED: (no output) exit 1
# ACTUAL (2026-10-27): no output, exit 1 — clean

# 3. No 0.0.0.0 listener (expect: only comments/tests — the guarded opt-in lives in cmd/)
grep -rn '0\.0\.0\.0' --include='*.go' internal/
# EXPECTED: comments/tests only (ports.go, hub comments, bind-absence tests)
# ACTUAL (2026-10-27): comments + ports/hub tests only — no listener

# 4. Frontend sends no auth headers of its own (expect: exit 1, no output)
grep -rni 'authorization\|setbasic\|withcredentials' web/src/
# EXPECTED: (no output) exit 1 — authentication rides on same-origin cookies, see §6
# ACTUAL (2026-10-27): no output, exit 1 — clean

# 5. Auth package has no logging path (expect: green)
go test -count=1 ./internal/auth/ -v
# EXPECTED: 22/22 pass incl. Test_Package_emits_no_logs + Test_Timing_compare_uses_constant_time
# ACTUAL (2026-10-27): 22 passed — green
```

Image/host checks — runnable now that the image + compose exist (run them on the
deploy host/VM after `./deploy.sh`; `<container>`/`<image>` are the running
dashboard container and `opencode-dashboard:0.1.0`):

```bash
# 6. Mount allowlist: exact output must equal §2 (no auth.json; data/cache/pi-sessions/localtime :ro, socket dir :rw)
docker inspect <container> --format '{{json .Mounts}}' | python3 -m json.tool
# EXPECTED: only the 5 §2 mounts; opencode/pi-sessions/zellij-cache/localtime
# "Mode": "ro" / "RW": false, socket dir "RW": true

# 7. No secret baked into image layers
docker history --no-trunc <image> | grep -i 'auth\|pass\|secret\|token\|credential'
# EXPECTED: (no output) exit 1

# 8. Loopback-only listen with DEFAULT config, no publish to misconfigure (host mode)
docker compose config | grep -E 'network_mode|ports:'
# EXPECTED: `network_mode: host` — and NO `ports:` line (grep finds only the network_mode hit)
ss -tlnp | grep $PORT
# EXPECTED (default BIND_ADDR): one 127.0.0.1:$PORT line owned by the dashboard binary,
# NO docker-proxy line, NO 0.0.0.0:$PORT line. A 0.0.0.0 line is valid ONLY with an
# explicit BIND_ADDR opt-in plus valid DASHBOARD_AUTH.
curl -s --max-time 5 http://127.0.0.1:$PORT/api/healthz
# EXPECTED: {"healthy":true,...}
```

## 6. SSE + Basic auth interaction (cookie bridge)

**Finding: native `EventSource` cannot carry Basic auth, so the server bridges
it with a bounded cookie. No `web/src` auth code is needed.**

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
- Same-origin **cookies** are sent by `EventSource`, so the gate mints one: a
  successful Basic check sets `ocd_session` (32 random bytes hex, `HttpOnly`,
  `SameSite=Lax`, `Path=/`, 12h TTL, bounded 32-token in-memory store, `Secure`
  only when the request arrived over TLS). `auth.Middleware` accepts valid
  Basic **or** a valid cookie; open mode issues no cookie. This is
  `internal/auth/session.go` + `basic.go`; the SPA's `sse.ts` is unchanged.
- Failure is loud: an unauthenticated `/api/events` returns 401; the client
  treats it as a drop and shows “reconnecting…”, never a fake live state.
  There is no login form and no per-user identity — the cookie is only proof
  that Basic succeeded.
- Explicitly out of scope: OAuth, multi-user accounts, persistent session
  storage, and CSRF-bearing mutations (v1 is read-only).

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
