package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixture list-panes -j JSON: one plugin pane (no CWD), one live opencode
// pane, one exited pane with an exit code, one pane with null
// terminal_command (live 0.44.3 shape: pane_command/pane_cwd carry truth).
const fixturePanesJSON = `[
  {"id": 0, "is_plugin": true, "title": "(.) - zellij:link",
   "terminal_command": null, "pane_command": null, "pane_cwd": null,
   "exited": false, "exit_status": null, "tab_id": 0, "tab_name": "Tab #1"},
  {"id": 1, "title": "OC | dashboard work",
   "terminal_command": "opencode", "pane_command": "opencode",
   "pane_cwd": "/proj/alpha", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"},
  {"id": 2, "title": "old task",
   "terminal_command": null, "pane_command": "make",
   "pane_cwd": "/proj/beta", "exited": true, "exit_status": 2,
   "tab_id": 1, "tab_name": "Tab #2"},
  {"id": 3, "title": "OC | null-terminal live shape",
   "terminal_command": null, "pane_command": "opencode",
   "pane_cwd": "/proj/gamma", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"}
]`

func Test_ParseSessionList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Session
	}{
		{
			name:  "short names only",
			input: "work-ptg\npersonal-laptop-2\n",
			want:  []Session{{Name: "work-ptg"}, {Name: "personal-laptop-2"}},
		},
		{
			name:  "exited suffix parses dead",
			input: "wa-scheduler (EXITED - attach to resurrect)\nwork-ptg\n",
			want:  []Session{{Name: "wa-scheduler", Exited: true}, {Name: "work-ptg"}},
		},
		{
			name: "full format with created timestamps and ansi",
			input: "\x1b[32;1mwa-scheduler\x1b[m [Created \x1b[35;1m1month 19days\x1b[m ago] " +
				"(\x1b[31;1mEXITED\x1b[m - attach to resurrect)\n" +
				"\x1b[32;1mwork-ptg\x1b[m [Created \x1b[35;1m1day 19h\x1b[m ago] \n",
			want: []Session{{Name: "wa-scheduler", Exited: true}, {Name: "work-ptg"}},
		},
		{
			name:  "current session marker stripped",
			input: "personal-laptop-2 [Created 4h ago] (current)\n",
			want:  []Session{{Name: "personal-laptop-2"}},
		},
		{
			name:  "blank lines skipped",
			input: "\n\nwork-ptg\n\n",
			want:  []Session{{Name: "work-ptg"}},
		},
		{
			name:  "empty output",
			input: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			got := ParseSessionList(tt.input)

			// Then
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("row %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func Test_ParsePanesJSON_fields(t *testing.T) {
	// When
	panes, err := ParsePanesJSON([]byte(fixturePanesJSON))

	// Then
	if err != nil {
		t.Fatalf("ParsePanesJSON: %v", err)
	}
	if len(panes) != 4 {
		t.Fatalf("got %d panes, want 4", len(panes))
	}
	if panes[1].Command != "opencode" || panes[1].CWD != "/proj/alpha" || panes[1].Exited {
		t.Errorf("live opencode pane wrong: %+v", panes[1])
	}
	if !panes[2].Exited {
		t.Errorf("exited pane must read dead: %+v", panes[2])
	}
	if panes[2].ExitStatus == nil || *panes[2].ExitStatus != 2 {
		t.Errorf("exit_status wrong: %+v", panes[2])
	}
	if panes[3].Command != "opencode" || panes[3].CWD != "/proj/gamma" {
		t.Errorf("null terminal_command must fall back to pane_command: %+v", panes[3])
	}
	if panes[0].CWD != "" || panes[0].Command != "" {
		t.Errorf("plugin pane must have empty command/cwd: %+v", panes[0])
	}
}

func Test_ParsePanesJSON_rejects_invalid_json(t *testing.T) {
	// When
	_, err := ParsePanesJSON([]byte("{not json"))

	// Then
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func Test_ListSessions_timeout_maps_typed_error(t *testing.T) {
	// Given: a runner that hangs until the context deadline fires.
	run := func(ctx context.Context, bin string, args []string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c := NewZellijClientWithRunner(ZellijConfig{Timeout: 50 * time.Millisecond}, run)

	// When
	_, err := c.ListSessions(context.Background())

	// Then
	if !errors.Is(err, ErrZellijTimeout) {
		t.Fatalf("got %v, want ErrZellijTimeout", err)
	}
}

func Test_ListSessions_missing_binary_maps_typed_error(t *testing.T) {
	// Given: a runner failing like exec does for a missing binary.
	run := func(ctx context.Context, bin string, args []string) ([]byte, error) {
		return nil, &exec.Error{Name: bin, Err: exec.ErrNotFound}
	}
	c := NewZellijClientWithRunner(ZellijConfig{}, run)

	// When
	_, err := c.ListSessions(context.Background())

	// Then
	if !errors.Is(err, ErrZellijMissing) {
		t.Fatalf("got %v, want ErrZellijMissing", err)
	}
}

func Test_ListSessions_exec_failure_maps_typed_error(t *testing.T) {
	// Given
	run := func(ctx context.Context, bin string, args []string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	}
	c := NewZellijClientWithRunner(ZellijConfig{}, run)

	// When
	_, err := c.ListSessions(context.Background())

	// Then
	if !errors.Is(err, ErrZellijExec) {
		t.Fatalf("got %v, want ErrZellijExec", err)
	}
}

func Test_SocketLive_missing_dir_maps_typed_error(t *testing.T) {
	// Given
	c := NewZellijClient(ZellijConfig{SocketDir: "/nonexistent-zellij-sockets-xyz"})

	// When
	_, err := c.SocketLive()

	// Then
	if !errors.Is(err, ErrSocketDirMissing) {
		t.Fatalf("got %v, want ErrSocketDirMissing", err)
	}
}

func createSocketFile(dir, name string) (string, error) {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		return "", err
	}
	return p, nil
}

func Test_SocketLive_lists_entries(t *testing.T) {
	// Given
	dir := t.TempDir()
	for _, name := range []string{"sess-a", "sess-b"} {
		f, err := createSocketFile(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		_ = f
	}
	c := NewZellijClient(ZellijConfig{SocketDir: dir})

	// When
	live, err := c.SocketLive()

	// Then
	if err != nil {
		t.Fatalf("SocketLive: %v", err)
	}
	if !live["sess-a"] || !live["sess-b"] || len(live) != 2 {
		t.Fatalf("got %v", live)
	}
	if !strings.Contains(c.cfg.SocketDir, dir) {
		t.Fatalf("client lost SocketDir config: %q", c.cfg.SocketDir)
	}
}
