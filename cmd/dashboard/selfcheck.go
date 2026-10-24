package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"

	"opencode-dashboard/internal/ports"
)

// ErrNonLoopbackBind is returned when the configured bind address is not a
// loopback address. The dashboard must never listen on 0.0.0.0.
var ErrNonLoopbackBind = errors.New("dashboard: bind address is not loopback")

// assertLoopback refuses any bind address that is not loopback. Config has
// no bind-addr env override by design; this is defense-in-depth so a future
// refactor can never silently expose the dashboard on 0.0.0.0.
func assertLoopback(cfg ports.Config) error {
	ip := net.ParseIP(cfg.BindAddr)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("bind %q: %w (127.0.0.1 ONLY — front with `tailscale serve`, never `funnel`)", cfg.BindAddr, ErrNonLoopbackBind)
	}
	return nil
}

// startupInfo carries the deploy facts selfChecks verified at boot.
type startupInfo struct {
	zellijVersion string
	zellijMajor   string
}

// selfChecks enforces deploy-time invariants LOUDLY: any failure returns an
// error carrying the exact remediation and run() exits non-zero. Silent
// degradation (blind dashboard) is banned for misconfig.
func selfChecks(cfg ports.Config) (*startupInfo, error) {
	slog.Info("startup identity", slog.Int("uid", os.Getuid()), slog.Int("gid", os.Getgid()))
	xdg := os.Getenv("XDG_RUNTIME_DIR")
	slog.Info("runtime dir", slog.String("xdg_runtime_dir", xdg))
	if xdg == "" {
		return nil, errors.New("XDG_RUNTIME_DIR is empty — export XDG_RUNTIME_DIR=/run/user/$(id -u) (verify the host value with `echo $XDG_RUNTIME_DIR`); the zellij CLI resolves its sockets from it")
	}
	if st, err := os.Stat(cfg.DataDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("DATA_DIR %s unreadable: %v — check the :ro mount and UID parity (container UID=%d vs host UID from `id -u`)", cfg.DataDir, err, os.Getuid())
	}
	dbPath := cfg.DataDir + "/opencode.db"
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("opencode.db missing at %s: %v — mount the parent opencode/ dir (db+-wal+-shm siblings must stay together) read-only to /data/opencode", dbPath, err)
	}
	bin, err := exec.LookPath("zellij")
	if err != nil {
		return nil, fmt.Errorf("zellij binary not found in PATH: %w — bake the zellij CLI (same major as host) into the image; without it the dashboard is blind to panes", err)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("zellij --version failed: %w — the baked CLI must run inside the image", err)
	}
	version := strings.TrimSpace(string(out))
	info := &startupInfo{zellijVersion: version, zellijMajor: zellijMajor(version)}
	slog.Info("zellij detected", slog.String("version", version), slog.String("major", info.zellijMajor), slog.String("bin", bin))
	return info, nil
}

// zellijMajor parses "zellij 0.44.3" into "0" for the socket-contract check:
// the socket dir (contract_version_1) is version-sensitive, so the baked
// CLI major must match the host's.
func zellijMajor(version string) string {
	fields := strings.Fields(version)
	if len(fields) < 2 {
		return ""
	}
	return strings.SplitN(fields[1], ".", 2)[0]
}
