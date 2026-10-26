package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	// countsMu guards lastLivePanes/lastLiveProcesses: the most recent
	// ground-truth observation, served via LiveCounts.
	countsMu          sync.Mutex
	lastLivePanes     int
	lastLiveProcesses int
	// warnedPanes/warnedProcs rate-limit degradation warnings to once per
	// process per source (atomic CAS: concurrent ticks warn exactly once).
	// Silent zeros hid a whole deploy; the first fault of each source logs.
	warnedPanes atomic.Bool
	warnedProcs atomic.Bool
	// warn reports one degradation line. A field (not a global) so tests
	// inject a counter; production logs via slog.
	warn func(msg string)
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
		warn:          func(msg string) { slog.Warn(msg) },
	}
}

var _ ports.RuntimeSource = (*Source)(nil)

// Enrich overlays pane liveness onto rows, preserving input order. Join key
// is CWD/directory (filepath.Clean for matching; raw values reported).
// Directory-scoped honesty: ONE process acts on ONE session, so at most the
// directory's most recently updated session (smallest UpdatedRelativeMs)
// claims the live pane; every other session in the same directory passes
// through DB-only with no pane claim and is NOT marked orphan. Orphan keeps
// its narrow meaning: an opencode process running outside zellij, claimed
// by the same newest-first rule.
//
// Row outcomes for the chosen session per directory:
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
// never crash the collector. The two presence sources are independent: a
// pane-listing fault never suppresses the process scan and vice versa. Each
// successful observation refreshes its LiveCounts component; a failed source
// keeps its last-known value and logs one warning line per process
// (warn-once, not per tick).
func (s *Source) Enrich(ctx context.Context, rows []model.SessionRow) ([]model.SessionRow, error) {
	out := make([]model.SessionRow, len(rows))
	copy(out, rows)
	if len(out) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// The two sources are observed independently: either may fail while
	// the other succeeds, and the join below uses whatever succeeded.
	livePanes, paneErr := s.listLivePanes(ctx)
	if paneErr != nil {
		s.warnOnce(&s.warnedPanes, fmt.Sprintf("runtime: pane listing failed, pane presence degraded (zellij socket-dir must be writable, XDG_RUNTIME_DIR parity, pid:host): %v", paneErr))
	}
	procs, procErr := ScanOpencode(s.procRoot)
	if procErr != nil {
		s.warnOnce(&s.warnedProcs, fmt.Sprintf("runtime: process scan failed, process presence degraded (needs pid:host /proc visibility): %v", procErr))
	}

	liveCount := 0
	panesByDir := make(map[string]Pane)
	if paneErr == nil {
		for _, np := range livePanes {
			if !np.pane.Exited {
				liveCount++
			}
			key := filepath.Clean(np.pane.CWD)
			if existing, ok := panesByDir[key]; !ok || (existing.Exited && !np.pane.Exited) {
				panesByDir[key] = np.pane
			}
		}
	}

	// Orphan scan: opencode procs with no zellij pane at their CWD.
	var procsByDir map[string][]ProcInfo
	procCount := -1
	if procErr == nil {
		procCount = 0
		procsByDir = make(map[string][]ProcInfo)
		for _, pr := range procs {
			if pr.Alive() {
				procCount++
			}
			if pr.CWD == "" {
				continue
			}
			key := filepath.Clean(pr.CWD)
			procsByDir[key] = append(procsByDir[key], pr)
		}
	}
	s.countsMu.Lock()
	if paneErr == nil {
		s.lastLivePanes = liveCount
	}
	if procCount >= 0 {
		s.lastLiveProcesses = procCount
	}
	s.countsMu.Unlock()

	// Newest-first: only the directory's most recently updated session may
	// claim presence. UpdatedRelativeMs is now-minus-time_updated, so the
	// minimum wins; ties keep input order (strict less-than replaces).
	claimant := make(map[string]int)
	for i := range out {
		if out[i].Directory == "" {
			continue
		}
		key := filepath.Clean(out[i].Directory)
		if j, ok := claimant[key]; !ok || out[i].UpdatedRelativeMs < out[j].UpdatedRelativeMs {
			claimant[key] = i
		}
	}
	claimed := make(map[int]struct{}, len(claimant))
	for _, i := range claimant {
		claimed[i] = struct{}{}
	}

	for i := range out {
		if out[i].Directory == "" {
			continue
		}
		if _, ok := claimed[i]; !ok {
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

// LiveCounts reports the last observed ground truth: live (non-exited)
// zellij panes and alive opencode processes. Zero before the first
// successful Enrich; last-known per source across infra faults (a failed
// source never clobbers the other source's good observation).
func (s *Source) LiveCounts() (panes int, procs int) {
	s.countsMu.Lock()
	defer s.countsMu.Unlock()
	return s.lastLivePanes, s.lastLiveProcesses
}

// warnOnce logs the first fault of one source only. The atomic CAS keeps
// concurrent ticks (and tests) to exactly one line per source per process.
func (s *Source) warnOnce(flag *atomic.Bool, msg string) {
	if flag.CompareAndSwap(false, true) && s.warn != nil {
		s.warn(msg)
	}
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
