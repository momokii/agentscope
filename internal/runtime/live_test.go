package runtime

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveProbe runs the REAL zellij CLI on this machine: list-sessions plus
// one list-panes per live session, printing counts only (no full dumps).
// Env-gated: OPENDASH_LIVE=1 go test ./internal/runtime/ -run TestLiveProbe -v
func TestLiveProbe(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") == "" {
		t.Skip("set OPENDASH_LIVE=1 to run the live zellij probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := DefaultConfig()
	t.Logf("socket dir: %s proc root: %s timeout: %s", cfg.SocketDir, cfg.ProcRoot, cfg.Timeout)
	src := NewSource(cfg)

	sessions, err := src.zellij.ListSessions(ctx)
	if err != nil {
		t.Fatalf("live list-sessions: %v", err)
	}
	live, exited := 0, 0
	for _, s := range sessions {
		if s.Exited {
			exited++
		} else {
			live++
		}
	}
	t.Logf("sessions: total=%d live=%d exited=%d", len(sessions), live, exited)

	sockets, err := src.zellij.SocketLive()
	if err != nil {
		t.Logf("socket dir: %v", err)
	} else {
		t.Logf("socket entries: %d", len(sockets))
	}

	procs, err := ScanOpencode(cfg.ProcRoot)
	if err != nil {
		t.Logf("proc scan: %v", err)
	} else {
		t.Logf("opencode procs: %d", len(procs))
	}

	queried := 0
	for _, s := range sessions {
		if s.Exited {
			continue
		}
		if sockets != nil {
			if _, ok := sockets[s.Name]; !ok {
				t.Logf("session %q has no socket entry (dead)", s.Name)
				continue
			}
		}
		panes, err := src.zellij.ListPanes(ctx, s.Name)
		if err != nil {
			t.Logf("list-panes %q: %v", s.Name, err)
			continue
		}
		queried++
		opencode := 0
		for _, p := range panes {
			if p.Command == "opencode" {
				opencode++
			}
		}
		t.Logf("session %q: panes=%d opencode=%d", s.Name, len(panes), opencode)
		if queried >= 3 {
			break
		}
	}
	t.Logf("queried %d live sessions (one list-panes each)", queried)
}
