package runtime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// fixtureCollisionPanes serves two sessions sharing one CWD: sess-a holds an
// exited pane at /proj/x, sess-b holds the live one. ResolvePane must prefer
// the live pane.
const fixtureCollisionLive = `[{"id": 7, "title": "OC | live",
	"terminal_command": "opencode", "pane_command": "opencode",
	"pane_cwd": "/proj/x", "exited": false, "exit_status": null,
	"tab_id": 0, "tab_name": "Tab #1"}]`

const fixtureCollisionExited = `[{"id": 3, "title": "old task",
	"terminal_command": "opencode", "pane_command": "opencode",
	"pane_cwd": "/proj/x/", "exited": true, "exit_status": 1,
	"tab_id": 0, "tab_name": "Tab #1"}]`

// fixtureExitedOnlyPanes serves one session whose only pane at /proj/dead is
// exited: resolution must report ErrNoLivePane, never the dead pane.
const fixtureExitedOnlyPanes = `[{"id": 4, "title": "dead task",
	"terminal_command": "make", "pane_command": "make",
	"pane_cwd": "/proj/dead", "exited": true, "exit_status": 2,
	"tab_id": 0, "tab_name": "Tab #1"}]`

func Test_ResolvePane_matches_cwd_with_clean(t *testing.T) {
	// Given: session directory without trailing slash, pane CWD with one.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePanesJSON},
	}
	src := testSourceWithDump(t, f, &fakeDump{})

	// When
	got, err := src.ResolvePane(context.Background(), "/proj/alpha/")

	// Then
	if err != nil {
		t.Fatalf("ResolvePane: %v", err)
	}
	if got.Session != "main" || got.PaneID != "1" {
		t.Fatalf("got %+v, want session main pane 1", got)
	}
	if got.CWD != "/proj/alpha" {
		t.Fatalf("CWD = %q, want raw pane value /proj/alpha", got.CWD)
	}
}

func Test_ResolvePane_prefers_live_on_cwd_collision(t *testing.T) {
	// Given: sess-a holds an exited pane at /proj/x, sess-b the live one.
	f := &fakeZellij{
		sessionsOut: "sess-a\nsess-b\n",
		panes: map[string]string{
			"sess-a": fixtureCollisionExited,
			"sess-b": fixtureCollisionLive,
		},
	}
	src := testSourceWithDump(t, f, &fakeDump{})

	// When
	got, err := src.ResolvePane(context.Background(), "/proj/x")

	// Then
	if err != nil {
		t.Fatalf("ResolvePane: %v", err)
	}
	if got.Session != "sess-b" || got.PaneID != "7" {
		t.Fatalf("got %+v, want live pane sess-b/7", got)
	}
}

func Test_ResolvePane_exited_only_maps_no_live_pane(t *testing.T) {
	// Given: only an exited pane matches /proj/dead.
	f := &fakeZellij{
		sessionsOut: "sess-dead\n",
		panes:       map[string]string{"sess-dead": fixtureExitedOnlyPanes},
	}
	src := testSourceWithDump(t, f, &fakeDump{})

	// When
	_, err := src.ResolvePane(context.Background(), "/proj/dead")

	// Then
	if !errors.Is(err, ErrNoLivePane) {
		t.Fatalf("got %v, want ErrNoLivePane", err)
	}
}

func Test_ResolvePane_no_match_maps_no_live_pane(t *testing.T) {
	// Given: live panes exist, none at the wanted directory.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePanesJSON},
	}
	src := testSourceWithDump(t, f, &fakeDump{})

	// When
	_, err := src.ResolvePane(context.Background(), "/proj/nowhere")

	// Then
	if !errors.Is(err, ErrNoLivePane) {
		t.Fatalf("got %v, want ErrNoLivePane", err)
	}
}

func Test_ResolvePane_propagates_listing_faults(t *testing.T) {
	// Given: zellij CLI exploding on every call (infra fault, not orphan).
	f := &fakeZellij{sessionsErr: errBoom(), sessionsOut: ""}
	src := testSourceWithDump(t, f, &fakeDump{})

	// When
	_, err := src.ResolvePane(context.Background(), "/proj/alpha")

	// Then: typed infra error, never misreported as no-live-pane.
	if errors.Is(err, ErrNoLivePane) {
		t.Fatalf("infra fault must not map to ErrNoLivePane: %v", err)
	}
	if !errors.Is(err, ErrZellijExec) {
		t.Fatalf("got %v, want ErrZellijExec", err)
	}
}

func Test_DumpSessionScreen_scopes_dump_to_session(t *testing.T) {
	// Given: collision fixture resolving to sess-b/7, fixed clock.
	fixed := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f := &fakeZellij{
		sessionsOut: "sess-a\nsess-b\n",
		panes: map[string]string{
			"sess-a": fixtureCollisionExited,
			"sess-b": fixtureCollisionLive,
		},
	}
	d := &fakeDump{stdout: []byte("live pane text")}
	src := testSourceWithDump(t, f, d)
	src.now = func() time.Time { return fixed }

	// When
	pane, text, err := src.DumpSessionScreen(context.Background(), "/proj/x", true)

	// Then
	if err != nil {
		t.Fatalf("DumpSessionScreen: %v", err)
	}
	if text != "live pane text" {
		t.Fatalf("got %q, want live pane text", text)
	}
	if pane.Session != "sess-b" || pane.PaneID != "7" {
		t.Fatalf("got %+v, want sess-b/7", pane)
	}
	if !pane.CapturedAt.Equal(fixed) {
		t.Fatalf("CapturedAt = %v, want %v", pane.CapturedAt, fixed)
	}
	want := []string{"--session", "sess-b", "action", "dump-screen", "--pane-id", "7", "--full"}
	got := d.calls[0]
	if len(got) != len(want) {
		t.Fatalf("dump args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dump args = %v, want %v", got, want)
		}
	}
}

// TestLiveDumpScreen dumps ONE real pane on this machine: the first live
// non-exited pane with a CWD. Env-gated:
// OPENDASH_LIVE=1 go test ./internal/runtime/ -run TestLiveDumpScreen -v
// It asserts <2s and non-empty output, then exits. Only counts, timings,
// and byte lengths are logged — never pane text (may contain secrets).
func TestLiveDumpScreen(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") == "" {
		t.Skip("set OPENDASH_LIVE=1 to run the live dump-screen probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	src := NewSource(DefaultConfig())
	panes, err := src.listLivePanes(ctx)
	if err != nil {
		t.Fatalf("live list panes: %v", err)
	}
	considered := len(panes)
	var target *namedPane
	for _, np := range panes {
		if !np.pane.Exited {
			np := np
			target = &np
			break
		}
	}
	if target == nil {
		t.Skipf("no live non-exited pane (considered %d)", considered)
	}

	start := time.Now()
	rp, text, err := src.DumpSessionScreen(ctx, target.pane.CWD, true)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("live dump: %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("live dump took %v, want <2s", elapsed)
	}
	if len(text) == 0 {
		t.Fatalf("live dump returned empty output")
	}
	t.Logf("considered=%d session=%q pane=%s capture_ms=%d bytes=%d",
		considered, rp.Session, rp.PaneID, elapsed.Milliseconds(), len(text))
}
