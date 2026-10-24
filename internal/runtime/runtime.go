package runtime

import (
	"context"
	"path/filepath"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// Config tunes the runtime source. Zero value is usable via DefaultConfig.
type Config struct {
	// Bin is the zellij binary (default "zellij").
	Bin string
	// RuntimeDir forces XDG_RUNTIME_DIR for every zellij exec.
	RuntimeDir string
	// SocketDir lists live sessions (default
	// $XDG_RUNTIME_DIR/zellij/contract_version_1).
	SocketDir string
	// ProcRoot is the proc filesystem root (default /proc; tests inject a
	// fixture tree).
	ProcRoot string
	// Timeout bounds each zellij listing exec (default DefaultTimeout).
	Timeout time.Duration
	// ScreenTimeout bounds each on-demand dump-screen exec (default
	// DefaultScreenTimeout). Listing stays on Timeout: the per-tick
	// collector budget must not grow because the viewer path allows more.
	ScreenTimeout time.Duration
}

// DefaultConfig resolves socket/proc/timeout defaults from the environment.
func DefaultConfig() Config {
	z := DefaultZellijConfig()
	return Config{
		Bin:           z.Bin,
		RuntimeDir:    z.RuntimeDir,
		SocketDir:     z.SocketDir,
		ProcRoot:      DefaultProcRoot,
		Timeout:       DefaultTimeout,
		ScreenTimeout: DefaultScreenTimeout,
	}
}

// Source implements ports.RuntimeSource: the zellij + /proc presence join.
// It is read-only: it never kills, attaches, or resurrects sessions.
// Source also owns the on-demand screen path (see screen.go): ResolvePane,
// DumpScreen, and DumpSessionScreen. The screen path is HTTP-driven only —
// never per tick, never from the collector.
type Source struct {
	zellij        *ZellijClient
	procRoot      string
	timeout       time.Duration
	screenTimeout time.Duration
	// now stamps screen captures; a field (not time.Now) so tests pin it.
	now func() time.Time
	// dump executes dump-screen invocations (see screen.go for why it is
	// a separate seam from Runner: zellij exits 0 on missing sessions, so
	// stderr text — not the exit code — is the failure signal).
	dump dumpRunner
}

// NewSource builds a Source with real zellij exec.
func NewSource(cfg Config) *Source {
	return NewSourceWithRunner(cfg, nil)
}

// NewSourceWithRunner builds a Source with an injected zellij Runner
// (tests). A nil run keeps real exec. The dump-screen seam always starts on
// real exec; same-package tests override it per-test.
func NewSourceWithRunner(cfg Config, run Runner) *Source {
	if cfg.ProcRoot == "" {
		cfg.ProcRoot = DefaultProcRoot
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.ScreenTimeout <= 0 {
		cfg.ScreenTimeout = DefaultScreenTimeout
	}
	return &Source{
		zellij: NewZellijClientWithRunner(ZellijConfig{
			Bin:        cfg.Bin,
			RuntimeDir: cfg.RuntimeDir,
			SocketDir:  cfg.SocketDir,
			Timeout:    cfg.Timeout,
		}, run),
		procRoot:      cfg.ProcRoot,
		timeout:       cfg.Timeout,
		screenTimeout: cfg.ScreenTimeout,
		now:           time.Now,
		dump:          defaultDumpRunner(cfg.RuntimeDir),
	}
}

var _ ports.RuntimeSource = (*Source)(nil)

// Enrich overlays pane liveness onto rows, preserving input order. Join key
// is CWD/directory (filepath.Clean for matching; raw values reported):
//
//   - pane hit: Pane.CWD is the live pane CWD, Exited is the pane exited
//     flag OR-ed with backing-proc death (zombie/missing opencode pid at the
//     same CWD). SessionRow.Directory is never overwritten, so a pane-CWD vs
//     directory mismatch stays visible as both values.
//   - orphan hit (opencode proc at the directory, no zellij pane): Orphan
//     true with empty Pane.CWD (dump-screen unavailable) and Exited true,
//     per the model contract.
//   - no hit: row passes through untouched (DB-only).
//
// Any infrastructure fault (zellij missing/timeout, unreadable socket dir or
// /proc) degrades to DB-only rows with a nil error: a broken zellij must
// never crash the collector.
func (s *Source) Enrich(ctx context.Context, rows []model.SessionRow) ([]model.SessionRow, error) {
	out := make([]model.SessionRow, len(rows))
	copy(out, rows)
	if len(out) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// One list-panes -j per live session max, via the shared listing path
	// (see listLivePanes in screen.go). A listing fault degrades to DB-only
	// rows: a broken zellij must never crash the collector.
	livePanes, err := s.listLivePanes(ctx)
	if err != nil {
		return out, nil
	}
	panesByDir := make(map[string]Pane)
	for _, np := range livePanes {
		key := filepath.Clean(np.pane.CWD)
		if existing, ok := panesByDir[key]; !ok || (existing.Exited && !np.pane.Exited) {
			panesByDir[key] = np.pane
		}
	}

	// Orphan scan: opencode procs with no zellij pane at their CWD.
	var procsByDir map[string][]ProcInfo
	if procs, err := ScanOpencode(s.procRoot); err == nil {
		procsByDir = make(map[string][]ProcInfo)
		for _, pr := range procs {
			if pr.CWD == "" {
				continue
			}
			key := filepath.Clean(pr.CWD)
			procsByDir[key] = append(procsByDir[key], pr)
		}
	}

	for i := range out {
		if out[i].Directory == "" {
			continue
		}
		key := filepath.Clean(out[i].Directory)
		if pane, ok := panesByDir[key]; ok {
			out[i].Pane.CWD = pane.CWD
			out[i].Pane.Exited = pane.Exited || backingDead(procsByDir[key])
			out[i].Orphan = false
			continue
		}
		if len(procsByDir[key]) > 0 {
			out[i].Orphan = true
			out[i].Pane = model.PaneInfo{Exited: true}
		}
	}
	return out, nil
}

// backingDead reports whether every opencode proc observed at a pane CWD is
// a zombie. No observed proc means no verdict (false): a plain shell pane
// has no opencode child and must not read as dead.
func backingDead(procs []ProcInfo) bool {
	if len(procs) == 0 {
		return false
	}
	for _, pr := range procs {
		if pr.Alive() {
			return false
		}
	}
	return true
}

// SocketDir exposes the configured socket dir (diagnostics).
func (s *Source) SocketDir() string {
	return s.zellij.cfg.SocketDir
}
