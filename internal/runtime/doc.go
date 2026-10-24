// Package runtime implements ports.RuntimeSource: the zellij + /proc
// presence join for the fleet collector (Wave 1, task W1-2).
//
// Design (from .omo/notepads/opencode-dashboard/research-runtime-observability.md):
//
//   - Liveness signal #1 is socket-dir membership:
//     $XDG_RUNTIME_DIR/zellij/contract_version_1/<session-name> exists only for
//     live sessions; EXITED sessions have no entry.
//   - `list-sessions -s` rows carrying the "(EXITED - attach to resurrect)"
//     suffix parse as dead. With `-s` the suffix is usually absent, so the
//     socket-dir check is the deciding vote.
//   - One `list-panes -j` per live session per tick max; `dump-screen` is
//     on-demand only via screen.go (DumpScreen/ResolvePane/
//     DumpSessionScreen, HTTP-driven per user view) — never per tick,
//     never from the collector.
//   - /proc ground truth: readlink /proc/<pid>/cwd is the project truth;
//     State Z (zombie) counts as dead; a missing /proc entry counts as exited.
//
// Same-UID/XDG assumptions (socket files are 0700, CLI resolves sockets via
// XDG_RUNTIME_DIR): the process must run as the same UID that owns the zellij
// server with XDG_RUNTIME_DIR pointing at that user's runtime dir. A wrong UID
// or XDG dir degrades to DB-only rows (typed errors below), never a crash.
//
// Every zellij exec carries a context timeout (default 5s, configurable). Any
// infrastructure fault degrades Enrich to DB-only rows with a nil error; the
// typed errors are returned by ListSessions/ListPanes/SocketLive for
// diagnostics.
package runtime
