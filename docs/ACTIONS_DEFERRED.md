# Actions (deferred) — why v1 is read-only, and what a future wave needs

Task quote: "Write deferred abort/resurrect API sketch explaining read-only v1 (W4-4)."

## 1. Why v1 is read-only

**Security first:** v1 has no prompt-injection path and no shell exec from the UI.
The dashboard only reads (`opencode.db` read-only, `zellij list-panes -j`,
`dump-screen`, `/proc`). Any action endpoint weakens this guarantee, so actions
are deferred until discovery, auth, and failure semantics are nailed down.

**Correctness second:** opencode session status is **in-memory per server
process** — there is no persisted status column. `GET /session/status` answers
only for the server instance queried: a fresh `opencode serve --port 4096`
returned `{}` while three TUI sessions were busy elsewhere. The dashboard's
multi-pane view spans MANY TUI processes, so a single abort target is
ambiguous: which process owns session `:id`, and on which port does THAT
instance listen? Until instance discovery is solved, an abort button would
sometimes abort nothing and sometimes the wrong thing — worse than no button.

## 2. Abort sketch (future)

```
POST /api/sessions/:id/abort
  → resolve session → live pane → owning opencode process
  → route abort to THAT instance's API
     (POST <that-instance>/session/:id/abort, or equivalent)
  → 200 {aborted:true} | 404 {no live owner} | 409 {busy-unabortable} | 502 {route failed}
```

Open problems (each a QUESTION a future worker must answer, not a decision):

1. **Discovery:** how does the dashboard learn each TUI's API port? TUI
   processes hold two localhost listeners serving an unknown protocol
   ("Endpoint not supported", string absent from the binary) — can ANY
   per-instance HTTP route be trusted before this is resolved?
2. **Auth:** what credential does the dashboard present to those instances
   (`OPENCODE_SERVER_PASSWORD`? per-user? none on 127.0.0.1)? Where is it
   stored so it never leaks into image/logs/API?
3. **Result semantics:** what does success/failure look like — how do we
   confirm the run actually stopped (event tail? status poll?) vs merely
   accepted, and what does the UI show while abort is in flight?
4. **Abort-during-compaction:** what happens if abort lands while
   `time_compacting` is set — is it safe, rejected (409?), or corrupting?

## 3. Resurrect sketch (future)

```
POST /api/zellij/sessions/:name/resurrect
  → zellij action resurrect (re-runs recorded command from serialized layout)
```

**Semantics warning (operator-facing):** resurrect does NOT resume the dead
process — it RE-RUNS the recorded command, i.e. a FRESH `opencode`, with prior
in-memory state gone. This holds for server quit/crash/reboot (EXITED rows);
disconnect/detach needs no resurrect at all (server keeps running). The UI must
state the data-loss boundary explicitly ("restarts opencode, does not resume")
before confirming. Open questions: which layout/command gets re-run, and who
may trigger it (auth)?

## 4. Non-goals (explicitly out of scope, now and for the actions wave)

- **No prompt sending** (`POST /session/:id/message`, `/prompt_async`,
  steer/queue) from the UI — prompt-injection boundary.
- **No shell exec** from the UI (`/shell`, `/command` run paths, `zellij action
  write/run`) — remote-code-execution boundary.

A future wave proposing either must re-open the security model first.

## 5. Acceptance for a future actions wave

- Unit: owner-resolution tests (session → pane → PID → port) incl. orphan
  (no pane → 404) and zombie (`State: Z` → dead) cases.
- Unit: abort-response mapping tests (200/404/409/502 per §2).
- Manual proof: abort a busy session from the dashboard, show the run stopped
  via event-tail evidence; abort an idle session → clean no-op; abort during
  compaction → documented behavior.
- Manual proof: resurrect an EXITED session, show fresh-process evidence and
  the data-loss warning in the UI; `delete-session` prune path covered.
- Gate: Security Gate re-pass (auth story, no secret in image/logs/API,
  127.0.0.1-only retained) before any action route ships.

## 6. Sources

- `research-opencode-internals.md` §4 — per-instance status, `{}` probe,
  DB-derived signals, `POST /session/:id/abort` exists.
- `research-opencode-internals.md` §5 — `serve`/`web`/`attach` surface, auth
  via `OPENCODE_SERVER_PASSWORD`, TUI↔server HTTP+SSE architecture.
- `research-opencode-internals.md` §6-U1 — unknown-listener anomaly.
- `research-opencode-internals.md` §8 — read-only data strategy.
- `research-runtime-observability.md` §1 — daemon model + resurrect nuance,
  zellij#3531/#4805.
- `research-runtime-observability.md` §7 — orphans, stale/dead, EXITED handling.
- `.omo/plans/opencode-dashboard.md` W4-4 + D7 — deferred-actions acceptance.
