package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"opencode-dashboard/internal/model"
)

// DefaultTimeout bounds every zellij CLI exec so a wedged server can never
// hang the collector poll loop.
const DefaultTimeout = 5 * time.Second

// ExitedSuffix marks dead sessions in `list-sessions` output. EXITED sessions
// are serialized metadata only: resurrecting RE-RUNS the recorded commands,
// it never resumes the old processes.
const ExitedSuffix = "(EXITED - attach to resurrect)"

var (
	// ErrZellijMissing means the zellij CLI is not on PATH (or not baked
	// into the image). Degrade to DB-only rows.
	ErrZellijMissing = errors.New("runtime: zellij binary not found")
	// ErrSocketDirMissing means $XDG_RUNTIME_DIR/zellij/contract_version_1
	// cannot be listed (wrong UID or XDG_RUNTIME_DIR). Suffix parsing still
	// applies; socket liveness is skipped.
	ErrSocketDirMissing = errors.New("runtime: zellij socket dir missing")
	// ErrZellijTimeout means a zellij exec exceeded its context deadline.
	ErrZellijTimeout = errors.New("runtime: zellij exec timeout")
	// ErrZellijExec means zellij ran but exited non-zero.
	ErrZellijExec = errors.New("runtime: zellij exec failed")
)

// Session is one parsed `list-sessions -s` row.
type Session struct {
	// Name is the zellij session name.
	Name string
	// Exited is true when the row carries the EXITED suffix (dead).
	Exited bool
}

// ParseSessionList parses `list-sessions -s` output (one name per line) and
// tolerates full `list-sessions` lines ("name [Created ...] (EXITED ...)")
// plus ANSI color codes, since callers may pass either.
func ParseSessionList(output string) []Session {
	var sessions []Session
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(stripANSI(raw))
		if line == "" {
			continue
		}
		sessions = append(sessions, parseSessionLine(line))
	}
	return sessions
}

func parseSessionLine(line string) Session {
	if idx := strings.Index(line, ExitedSuffix); idx >= 0 {
		name := strings.TrimSpace(line[:idx])
		if fields := strings.Fields(name); len(fields) > 0 {
			name = fields[0]
		}
		return Session{Name: name, Exited: true}
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Session{}
	}
	if len(fields) == 1 {
		return Session{Name: fields[0]}
	}
	// Full-format line: "name [Created ...]" with optional "(current)".
	if strings.HasPrefix(fields[1], "[") {
		return Session{Name: fields[0]}
	}
	return Session{Name: line}
}

// Pane is one parsed `list-panes -j` entry.
type Pane struct {
	// ID is the zellij pane id (unique per session, not globally).
	ID int
	// TabID and TabName locate the pane in the session layout.
	TabID   int
	TabName string
	// Title is the pane title (opencode panes look like "OC | ...").
	Title string
	// Command is terminal_command, falling back to pane_command: live
	// servers (0.44.3 observed) report terminal_command null while
	// pane_command holds the foreground process ("opencode", "pi").
	Command string
	// PaneCommand is the raw pane_command (foreground process) before the
	// Command fallback. Harness classification reads this first: a pane
	// launched by one binary but running another (e.g. opencode spawning
	// a shell that runs pi) reports the truth here.
	PaneCommand string
	// CWD is the zellij-reported working directory (pane_cwd).
	CWD string
	// Exited mirrors the pane `exited` flag (dead pane in a live session).
	Exited bool
	// ExitStatus is the pane exit code when reported, nil otherwise.
	ExitStatus *int
}

// paneJSON is the boundary parse of one list-panes -j entry. Unknown fields
// are ignored so newer zellij versions keep parsing.
type paneJSON struct {
	ID              int     `json:"id"`
	TabID           int     `json:"tab_id"`
	TabName         string  `json:"tab_name"`
	Title           string  `json:"title"`
	TerminalCommand *string `json:"terminal_command"`
	PaneCommand     *string `json:"pane_command"`
	PaneCWD         *string `json:"pane_cwd"`
	Exited          bool    `json:"exited"`
	ExitStatus      *int    `json:"exit_status"`
}

// ParsePanesJSON parses `zellij --session <name> action list-panes -j`.
func ParsePanesJSON(data []byte) ([]Pane, error) {
	var raw []paneJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("runtime: decode list-panes JSON: %w", err)
	}
	panes := make([]Pane, 0, len(raw))
	for _, p := range raw {
		panes = append(panes, Pane{
			ID:          p.ID,
			TabID:       p.TabID,
			TabName:     p.TabName,
			Title:       p.Title,
			Command:     firstNonEmpty(deref(p.TerminalCommand), deref(p.PaneCommand)),
			PaneCommand: deref(p.PaneCommand),
			CWD:         deref(p.PaneCWD),
			Exited:      p.Exited,
			ExitStatus:  p.ExitStatus,
		})
	}
	return panes, nil
}

// Harness classifies the pane by its foreground process: pane_command
// first (the live truth), terminal_command as fallback. It deliberately
// never reads the pane title — pi's "π - <dir>" title is undocumented UI
// that may change, while pane_command is the observed process. Unknown
// foregrounds (shells, make, plugins) report "" and stay claimable by
// any harness row (legacy presence behavior); known harnesses claim only
// same-harness rows so one directory can host both harnesses.
func (p Pane) Harness() model.Harness {
	// PaneCommand is the foreground truth; Command (terminal-fallback-pane)
	// covers panes whose pane_command is absent.
	if h := harnessForCommand(p.PaneCommand); h != "" {
		return h
	}
	return harnessForCommand(p.Command)
}

// harnessForCommand maps an argv[0]-style command basename to its harness,
// or "" when the foreground is not a known agent.
func harnessForCommand(cmd string) model.Harness {
	switch filepath.Base(cmd) {
	case "opencode":
		return model.HarnessOpencode
	case "pi":
		return model.HarnessPi
	default:
		return ""
	}
}

// Runner executes one zellij invocation. It is a field (not a global) so
// tests inject fakes; production uses defaultRunner.
type Runner func(ctx context.Context, bin string, args []string) ([]byte, error)

// defaultRunner runs bin via exec with XDG_RUNTIME_DIR forced, so the CLI
// resolves the host socket dir even when the process env differs.
func defaultRunner(runtimeDir string) Runner {
	return func(ctx context.Context, bin string, args []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		if runtimeDir != "" {
			cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return nil, fmt.Errorf("%s: %w", msg, err)
			}
			return nil, err
		}
		return stdout.Bytes(), nil
	}
}

// ZellijConfig tunes the zellij client. Zero value is usable via
// DefaultZellijConfig.
type ZellijConfig struct {
	// Bin is the zellij binary (default "zellij").
	Bin string
	// RuntimeDir forces XDG_RUNTIME_DIR for every exec.
	RuntimeDir string
	// SocketDir lists live sessions (default
	// $XDG_RUNTIME_DIR/zellij/contract_version_1).
	SocketDir string
	// Timeout bounds each exec (default DefaultTimeout).
	Timeout time.Duration
}

// DefaultZellijConfig resolves dirs from XDG_RUNTIME_DIR, falling back to
// /run/user/<uid> (the systemd default).
func DefaultZellijConfig() ZellijConfig {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return ZellijConfig{
		Bin:        "zellij",
		RuntimeDir: runtimeDir,
		SocketDir:  runtimeDir + "/zellij/contract_version_1",
		Timeout:    DefaultTimeout,
	}
}

// ZellijClient is the read-only zellij observer. It never attaches,
// resurrects, kills, or otherwise mutates sessions.
type ZellijClient struct {
	cfg ZellijConfig
	run Runner
}

// NewZellijClient builds a client with real exec.
func NewZellijClient(cfg ZellijConfig) *ZellijClient {
	if cfg.Bin == "" {
		cfg.Bin = "zellij"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &ZellijClient{cfg: cfg, run: defaultRunner(cfg.RuntimeDir)}
}

// NewZellijClientWithRunner builds a client with an injected Runner
// (tests). A nil run keeps real exec.
func NewZellijClientWithRunner(cfg ZellijConfig, run Runner) *ZellijClient {
	c := NewZellijClient(cfg)
	if run != nil {
		c.run = run
	}
	return c
}

// withTimeout bounds one exec; callers must defer cancel.
func (c *ZellijClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.cfg.Timeout)
}

// ListSessions runs `zellij list-sessions -s` and parses EXITED rows as dead.
func (c *ZellijClient) ListSessions(ctx context.Context) ([]Session, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.run(ctx, c.cfg.Bin, []string{"list-sessions", "-s"})
	if err != nil {
		return nil, mapExecError(ctx, c.cfg.Bin, err)
	}
	return ParseSessionList(string(out)), nil
}

// ListPanes runs one `list-panes -j` for a live session.
func (c *ZellijClient) ListPanes(ctx context.Context, session string) ([]Pane, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.run(ctx, c.cfg.Bin, []string{"--session", session, "action", "list-panes", "-j"})
	if err != nil {
		return nil, mapExecError(ctx, c.cfg.Bin, err)
	}
	panes, err := ParsePanesJSON(out)
	if err != nil {
		return nil, err
	}
	return panes, nil
}

// SocketLive lists the socket dir: membership is liveness signal #1.
// Absence of an entry means the session is dead.
func (c *ZellijClient) SocketLive() (map[string]bool, error) {
	entries, err := os.ReadDir(c.cfg.SocketDir)
	if err != nil {
		return nil, fmt.Errorf("runtime: read socket dir %s: %w", c.cfg.SocketDir, ErrSocketDirMissing)
	}
	live := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Name() != "" {
			live[e.Name()] = true
		}
	}
	return live, nil
}

// mapExecError classifies runner failures into typed errors.
func mapExecError(ctx context.Context, bin string, err error) error {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("runtime: exec %s: %w", bin, ErrZellijMissing)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return fmt.Errorf("runtime: exec %s: %w", bin, ErrZellijTimeout)
	case ctx.Err() != nil:
		return fmt.Errorf("runtime: exec %s: %w", bin, ErrZellijTimeout)
	default:
		return fmt.Errorf("runtime: exec %s: %w", bin, ErrZellijExec)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// stripANSI drops ESC[...letter sequences from CLI output.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
