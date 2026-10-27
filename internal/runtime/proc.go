package runtime

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultProcRoot is the host /proc. In containers this works via --pid=host;
// tests inject a fixture tree instead (never the live system in unit tests).
const DefaultProcRoot = "/proc"

// ErrProcGone means /proc/<pid> is missing: the process fully exited.
var ErrProcGone = errors.New("runtime: proc entry missing (exited)")

// ErrNoChild means no child of the given PPID matches the wanted command.
var ErrNoChild = errors.New("runtime: no matching child proc")

// Liveness is the host-side aliveness of one PID.
type Liveness string

const (
	// LivenessAlive covers R (on-CPU), S (sleeping), D (uninterruptible)
	// and other scheduled states.
	LivenessAlive Liveness = "alive"
	// LivenessZombie covers Z (zombie, awaiting reaping) and X/x (dead):
	// the process is gone even though a /proc entry lingers.
	LivenessZombie Liveness = "zombie"
	// LivenessGone means no /proc entry at all (fully exited).
	LivenessGone Liveness = "gone"
)

// ClassifyState maps a /proc status State letter to Liveness. R=on-CPU,
// S=sleeping (the normal opencode steady state), D=uninterruptible IO count
// as alive; Z=zombie counts as DEAD; X/x count as dead.
func ClassifyState(state byte) Liveness {
	switch state {
	case 'Z', 'X', 'x':
		return LivenessZombie
	default:
		return LivenessAlive
	}
}

// ProcInfo is one agent process observed via /proc.
type ProcInfo struct {
	// PID is the agent process id.
	PID int
	// Command is the argv[0] basename as observed ("opencode" or "pi").
	// It identifies the harness; the scan guarantees it is never empty.
	Command string
	// PPID is the parent pid (the pane shell for zellij-hosted sessions).
	PPID int
	// CWD is readlink /proc/<pid>/cwd: the project truth. Best-effort:
	// empty when the link cannot be read.
	CWD string
	// State is the raw State letter from /proc/<pid>/status.
	State byte
}

// Alive reports Classified liveness: zombies count as dead.
func (p ProcInfo) Alive() bool {
	return ClassifyState(p.State) == LivenessAlive
}

// ReadProcState returns the State letter from /proc/<pid>/status.
// Missing entry maps to ErrProcGone (exited).
func ReadProcState(procRoot string, pid int) (byte, error) {
	line, err := readStatusField(procRoot, pid, "State:")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0, fmt.Errorf("runtime: pid %d has empty State: %w", pid, ErrProcGone)
	}
	return fields[0][0], nil
}

// ReadProcCWD returns readlink /proc/<pid>/cwd. Missing entry maps to
// ErrProcGone (exited).
func ReadProcCWD(procRoot string, pid int) (string, error) {
	target, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "cwd"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("runtime: cwd pid %d: %w", pid, ErrProcGone)
		}
		return "", fmt.Errorf("runtime: readlink cwd pid %d: %w", pid, err)
	}
	return target, nil
}

// ReadProcPPID returns the PPid field from /proc/<pid>/status. Missing entry
// maps to ErrProcGone (exited).
func ReadProcPPID(procRoot string, pid int) (int, error) {
	line, err := readStatusField(procRoot, pid, "PPid:")
	if err != nil {
		return 0, err
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return 0, fmt.Errorf("runtime: pid %d bad PPid %q: %w", pid, line, err)
	}
	return ppid, nil
}

// PIDLiveness classifies one pid: alive, zombie (dead), or gone (exited).
func PIDLiveness(procRoot string, pid int) Liveness {
	state, err := ReadProcState(procRoot, pid)
	if err != nil {
		return LivenessGone
	}
	if ClassifyState(state) == LivenessZombie {
		return LivenessZombie
	}
	return LivenessAlive
}

// agentCommands is the argv[0] basename allowlist for agent processes.
// Both harnesses are matched so one /proc walk serves the merged fleet;
// unknown basenames never match (plain shells contribute no presence).
var agentCommands = map[string]bool{"opencode": true, "pi": true}

// ScanOpencode lists opencode processes: every numeric /proc entry whose
// argv[0] basename is "opencode". Racy entries (vanishing mid-scan) are
// skipped; only an unreadable proc root fails the scan.
func ScanOpencode(procRoot string) ([]ProcInfo, error) {
	return scanProcs(procRoot, func(base string) bool { return base == "opencode" })
}

// ScanAgents lists agent processes for every known harness (opencode and
// pi): every numeric /proc entry whose argv[0] basename is in the agent
// allowlist. One walk serves the merged fleet; ProcInfo.Command carries
// the harness for per-(directory,harness) attribution in Enrich.
func ScanAgents(procRoot string) ([]ProcInfo, error) {
	return scanProcs(procRoot, func(base string) bool { return agentCommands[base] })
}

// scanProcs is the shared /proc walker behind ScanOpencode and ScanAgents.
// match selects basenames; CWD stays best-effort (see ScanOpencode).
func scanProcs(procRoot string, match func(string) bool) ([]ProcInfo, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("runtime: read proc root %s: %w", procRoot, err)
	}
	var found []ProcInfo
	for _, e := range entries {
		if !isPIDDir(e.Name()) {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		argv0, err := readArgv0(procRoot, pid)
		base := path.Base(argv0)
		if err != nil || !match(base) {
			continue
		}
		state, err := ReadProcState(procRoot, pid)
		if err != nil {
			continue
		}
		ppid, err := ReadProcPPID(procRoot, pid)
		if err != nil {
			continue
		}
		// CWD is best-effort: any read failure (vanished mid-scan,
		// EACCES on readlink in-container, ...) keeps the entry with
		// an empty CWD rather than dropping the row.
		cwd, cwdErr := ReadProcCWD(procRoot, pid)
		if cwdErr != nil {
			cwd = ""
		}
		found = append(found, ProcInfo{PID: pid, PPID: ppid, CWD: cwd, State: state, Command: base})
	}
	return found, nil
}

// ChildPID resolves the pane-shell -> opencode PPID chain: the lowest child
// pid of ppid whose argv[0] basename matches base (e.g. "opencode").
// ReadDir returns sorted names, so the first match is deterministic.
func ChildPID(procRoot string, ppid int, base string) (int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, fmt.Errorf("runtime: read proc root %s: %w", procRoot, err)
	}
	for _, e := range entries {
		if !isPIDDir(e.Name()) {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		got, err := ReadProcPPID(procRoot, pid)
		if err != nil || got != ppid {
			continue
		}
		argv0, err := readArgv0(procRoot, pid)
		if err != nil || path.Base(argv0) != base {
			continue
		}
		return pid, nil
	}
	return 0, fmt.Errorf("runtime: ppid %d base %s: %w", ppid, base, ErrNoChild)
}

func readStatusField(procRoot string, pid int, key string) (string, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("runtime: status pid %d: %w", pid, ErrProcGone)
		}
		return "", fmt.Errorf("runtime: read status pid %d: %w", pid, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key) {
			return strings.TrimSpace(strings.TrimPrefix(line, key)), nil
		}
	}
	return "", fmt.Errorf("runtime: pid %d missing %s: %w", pid, key, ErrProcGone)
}

// readArgv0 returns /proc/<pid>/cmdline argv[0]; empty cmdline (kernel
// threads) is an error so they never match.
func readArgv0(procRoot string, pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("runtime: cmdline pid %d: %w", pid, ErrProcGone)
		}
		return "", fmt.Errorf("runtime: read cmdline pid %d: %w", pid, err)
	}
	if len(data) == 0 || data[0] == 0 {
		return "", fmt.Errorf("runtime: pid %d empty cmdline: %w", pid, ErrProcGone)
	}
	end := len(data)
	if i := strings.IndexByte(string(data), 0); i >= 0 {
		end = i
	}
	return string(data[:end]), nil
}

func isPIDDir(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	return true
}
