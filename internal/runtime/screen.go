package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"opencode-dashboard/internal/model"
)

// DefaultScreenTimeout bounds every dump-screen exec. It is deliberately
// longer than DefaultTimeout (5s): a --full scrollback dump serializes far
// more PTY content than list-sessions/list-panes, and the on-demand viewer
// path (one pane per user view) can afford 10s where the per-tick collector
// loop cannot.
const DefaultScreenTimeout = 10 * time.Second

var (
	// ErrNoLivePane means ResolvePane found no live pane for a session
	// directory: either nothing matches, or only orphans/EXITED panes
	// match. The hub maps this to 409 with a DB-only notice (never a fake
	// empty screen).
	ErrNoLivePane = errors.New("runtime: no live pane for session directory")
	// ErrPaneGone means a dump-screen exec proved the pane dead: zellij
	// prints a "not found" diagnostic (exit code is NOT reliable — a
	// missing session exits 0, verified live on zellij 0.44.3) or returns
	// effectively-empty output (a bogus pane id returns a lone "\n",
	// exit 0, empty stderr, verified live). A genuinely blank viewport
	// still dumps rows of spaces, so whitespace-empty output is the
	// gone-pane signature, not a blank screen.
	ErrPaneGone = errors.New("runtime: pane is gone")
	// ErrScreenTimeout means a dump-screen exec exceeded its context
	// deadline (default DefaultScreenTimeout, configurable via
	// Config.ScreenTimeout).
	ErrScreenTimeout = errors.New("runtime: dump-screen timeout")
)

// ResolvedPane is one live zellij pane matched to a session directory.
type ResolvedPane struct {
	// Session is the live zellij session name owning the pane. It scopes
	// the dump: numeric pane ids repeat across sessions, so an unscoped
	// `dump-screen --pane-id N` may read the wrong session's pane.
	Session string
	// PaneID is the decimal pane id passed to --pane-id.
	PaneID string
	// CWD is the raw pane CWD (un-cleaned, for display).
	CWD string
	// CapturedAt is stamped when the dump completes (UTC).
	CapturedAt time.Time
}

// namedPane is one live pane with its owning session name.
type namedPane struct {
	session string
	pane    Pane
}

// dumpRunner executes one dump-screen invocation, returning stdout and
// stderr separately. Separate streams are required (not the Runner seam:
// it drops stderr on success) because zellij exits 0 even when the session
// is missing — the stderr text is the failure signal, not the exit code.
// It is a Source field (not a global) so tests inject fakes; production
// uses defaultDumpRunner, which forces XDG_RUNTIME_DIR like defaultRunner.
type dumpRunner func(ctx context.Context, bin string, args []string) (stdout, stderr []byte, err error)

// defaultDumpRunner runs bin via exec with XDG_RUNTIME_DIR forced, so the
// CLI resolves the host socket dir even when the process env differs.
func defaultDumpRunner(runtimeDir string) dumpRunner {
	return func(ctx context.Context, bin string, args []string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		if runtimeDir != "" {
			cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return stdout.Bytes(), stderr.Bytes(), err
		}
		return stdout.Bytes(), stderr.Bytes(), nil
	}
}

// listLivePanes lists every pane of every live session: EXITED sessions
// and socket-absent sessions are dead and skipped, and a per-session
// list-panes failure degrades only that session. Empty-CWD panes (plugins)
// never match a directory and are dropped here. Callers: Enrich (which
// degrades infra faults to DB-only rows) and ResolvePane (which propagates
// them typed — an infra fault must never misreport as orphan).
func (s *Source) listLivePanes(ctx context.Context) ([]namedPane, error) {
	sessions, err := s.zellij.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	socketLive, err := s.zellij.SocketLive()
	socketOK := err == nil

	var out []namedPane
	for _, sess := range sessions {
		if sess.Exited {
			continue
		}
		if socketOK && !socketLive[sess.Name] {
			continue
		}
		panes, err := s.zellij.ListPanes(ctx, sess.Name)
		if err != nil {
			continue
		}
		for _, p := range panes {
			if p.CWD == "" {
				continue
			}
			out = append(out, namedPane{session: sess.Name, pane: p})
		}
	}
	return out, nil
}

// ResolvePane maps a session directory to its live zellij pane, reusing the
// W1-2 listing path (listLivePanes above — never a second implementation).
// Matching Cleans both sides (zellij may report a trailing slash). The
// harness disambiguates a directory hosting both agents: an unknown-
// foreground pane resolves for either harness (legacy), but a pane running
// a known agent resolves only for the same harness. On a CWD collision the
// first non-exited pane wins; when only exited panes match, or nothing
// matches, it returns ErrNoLivePane. Listing infra faults (missing binary,
// timeout) propagate typed so the caller can distinguish "no pane" from
// "zellij broken". An empty/unknown harness reads as opencode (the
// pre-Wave-5 contract had no harness dimension).
func (s *Source) ResolvePane(ctx context.Context, sessionDirectory string, harness model.Harness) (ResolvedPane, error) {
	if strings.TrimSpace(sessionDirectory) == "" {
		return ResolvedPane{}, fmt.Errorf("runtime: resolve pane for empty directory: %w", ErrNoLivePane)
	}
	if !harness.IsValid() {
		harness = model.HarnessOpencode
	}
	panes, err := s.listLivePanes(ctx)
	if err != nil {
		return ResolvedPane{}, err
	}
	want := filepath.Clean(sessionDirectory)
	var exitedOnly *namedPane
	for _, np := range panes {
		if filepath.Clean(np.pane.CWD) != want {
			continue
		}
		if !claimableBy(np.pane.Harness(), harness) {
			continue
		}
		if !np.pane.Exited {
			return ResolvedPane{
				Session: np.session,
				PaneID:  strconv.Itoa(np.pane.ID),
				CWD:     np.pane.CWD,
			}, nil
		}
		if exitedOnly == nil {
			np := np
			exitedOnly = &np
		}
	}
	if exitedOnly != nil {
		return ResolvedPane{}, fmt.Errorf("runtime: resolve pane for %q: only exited panes remain: %w", sessionDirectory, ErrNoLivePane)
	}
	return ResolvedPane{}, fmt.Errorf("runtime: resolve pane for %q: %w", sessionDirectory, ErrNoLivePane)
}

// DumpScreen dumps one pane by id and returns the output as plain text,
// whole: no truncation server-side. Truncating here would break --full
// scrollback consumers (search, tail-follow) and hide content the viewer
// may legitimately need; the drill-down viewer clamps display height
// instead. Transport is localhost-only (127.0.0.1 + tailscale serve), so
// full-pane text never crosses an untrusted boundary.
//
// Prefer DumpSessionScreen when a session directory is known: bare pane ids
// repeat across sessions, so this unscoped form may read the wrong
// session's pane. It exists for diagnostics and for callers that already
// hold a session-unique pane reference.
func (s *Source) DumpScreen(ctx context.Context, paneID string, full bool) (string, error) {
	if strings.TrimSpace(paneID) == "" {
		return "", fmt.Errorf("runtime: dump-screen with empty pane id")
	}
	args := []string{"action", "dump-screen", "--pane-id", paneID}
	if full {
		args = append(args, "--full")
	}
	return s.execDump(ctx, "", args)
}

// DumpSessionScreen resolves a session directory to its live pane and dumps
// it session-scoped (`--session <name> action dump-screen`), which is the
// only unambiguous form when numeric pane ids repeat across sessions. The
// harness selects the pane when both agents share the directory. It returns
// the resolved pane (for X-Pane-Id / X-Captured-At headers) and the whole
// dump text. This is the on-demand viewer path: the hub calls it per user
// view only, never per tick and never from the collector.
func (s *Source) DumpSessionScreen(ctx context.Context, sessionDirectory string, harness model.Harness, full bool) (ResolvedPane, string, error) {
	rp, err := s.ResolvePane(ctx, sessionDirectory, harness)
	if err != nil {
		return ResolvedPane{}, "", err
	}
	args := []string{"--session", rp.Session, "action", "dump-screen", "--pane-id", rp.PaneID}
	if full {
		args = append(args, "--full")
	}
	text, err := s.execDump(ctx, rp.Session, args)
	if err != nil {
		return ResolvedPane{}, "", err
	}
	rp.CapturedAt = s.now().UTC()
	return rp, text, nil
}

// execDump runs one dump-screen invocation under the hard screen timeout
// and classifies the result into typed errors.
func (s *Source) execDump(ctx context.Context, scope string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.screenTimeout)
	defer cancel()
	stdout, stderr, err := s.dump(ctx, s.zellij.cfg.Bin, args)
	if err != nil {
		switch {
		case dumpTimeoutHit(ctx, err):
			return "", fmt.Errorf("runtime: dump-screen: %w", ErrScreenTimeout)
		case errors.Is(err, exec.ErrNotFound):
			return "", fmt.Errorf("runtime: dump-screen %s: %w", scope, ErrZellijMissing)
		case isNotFoundText(stderr):
			return "", fmt.Errorf("runtime: dump-screen %s: %w", scope, ErrPaneGone)
		default:
			return "", fmt.Errorf("runtime: dump-screen %s: %w", scope, ErrZellijExec)
		}
	}
	// zellij exits 0 with a "not found" diagnostic on stderr when the
	// session is missing (verified live) — exit code alone proves nothing.
	if isNotFoundText(stderr) {
		return "", fmt.Errorf("runtime: dump-screen %s: %w", scope, ErrPaneGone)
	}
	// A bogus pane id exits 0 with a lone newline and empty stderr
	// (verified live). A genuinely blank viewport still dumps rows of
	// spaces, so whitespace-empty output is the gone-pane signature.
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "", fmt.Errorf("runtime: dump-screen %s: empty output: %w", scope, ErrPaneGone)
	}
	return string(stdout), nil
}

// dumpTimeoutHit reports whether a dump failure is a deadline hit: the
// runner's own deadline error, a cancelled context, or an already-expired
// request context (mirrors mapExecError's contract in zellij.go).
func dumpTimeoutHit(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		ctx.Err() != nil
}

// isNotFoundText matches zellij's dead-target diagnostics, e.g.
// "Session 'x' not found. The following sessions are active:".
func isNotFoundText(stderr []byte) bool {
	lowered := strings.ToLower(string(stderr))
	for _, marker := range []string{"not found", "no such pane", "couldn't find", "no pane"} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}
