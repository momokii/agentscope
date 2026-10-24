package runtime

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeDump captures dump-screen invocations and serves canned streams. Args
// are recorded so tests assert --full / --session scoping without running
// zellij.
type fakeDump struct {
	stdout []byte
	stderr []byte
	err    error
	calls  [][]string
	bins   []string
	block  bool
}

func (f *fakeDump) run(ctx context.Context, bin string, args []string) ([]byte, []byte, error) {
	f.bins = append(f.bins, bin)
	f.calls = append(f.calls, append([]string{}, args...))
	if f.block {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return f.stdout, f.stderr, f.err
}

func testSourceWithDump(t *testing.T, f *fakeZellij, d *fakeDump) *Source {
	t.Helper()
	sockets := []string{"main", "sess-a", "sess-b", "sess-dead"}
	src := testSource(t, f, sockets, nil)
	src.dump = d.run
	return src
}

func Test_DumpScreen_passes_full_flag_and_returns_text_whole(t *testing.T) {
	// Given: a 100KB dump (scrollback must survive the trip untruncated).
	big := strings.Repeat("0123456789abcdef\n", 5880)
	d := &fakeDump{stdout: []byte(big)}
	src := testSourceWithDump(t, &fakeZellij{}, d)

	// When
	got, err := src.DumpScreen(context.Background(), "7", true)

	// Then
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if got != big {
		t.Fatalf("dump len = %d, want %d (no server-side truncation)", len(got), len(big))
	}
	if len(d.calls) != 1 {
		t.Fatalf("dump calls = %d, want 1", len(d.calls))
	}
	args := d.calls[0]
	if args[0] != "action" || args[1] != "dump-screen" || args[2] != "--pane-id" || args[3] != "7" {
		t.Fatalf("dump args = %v, want action dump-screen --pane-id 7 ...", args)
	}
	if args[len(args)-1] != "--full" {
		t.Fatalf("dump args = %v, want trailing --full", args)
	}
}

func Test_DumpScreen_omits_full_flag_for_viewport(t *testing.T) {
	// Given
	d := &fakeDump{stdout: []byte("viewport text")}
	src := testSourceWithDump(t, &fakeZellij{}, d)

	// When
	got, err := src.DumpScreen(context.Background(), "7", false)

	// Then
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if got != "viewport text" {
		t.Fatalf("got %q, want viewport text", got)
	}
	for _, a := range d.calls[0] {
		if a == "--full" {
			t.Fatalf("viewport dump must not pass --full: %v", d.calls[0])
		}
	}
}

func Test_DumpScreen_empty_output_maps_pane_gone(t *testing.T) {
	// Given: zellij's verified bogus-pane reply — lone newline, exit 0.
	d := &fakeDump{stdout: []byte("\n")}
	src := testSourceWithDump(t, &fakeZellij{}, d)

	// When
	_, err := src.DumpScreen(context.Background(), "99999", true)

	// Then
	if !errors.Is(err, ErrPaneGone) {
		t.Fatalf("got %v, want ErrPaneGone", err)
	}
}

func Test_DumpScreen_session_not_found_maps_pane_gone(t *testing.T) {
	// Given: zellij's verified missing-session reply — exit 0 with the
	// diagnostic on stderr, so the exit code alone proves nothing.
	d := &fakeDump{
		stdout: []byte("personal-laptop-2\nwork-ptg\n"),
		stderr: []byte("Session 'ghost' not found. The following sessions are active:\n"),
	}
	src := testSourceWithDump(t, &fakeZellij{}, d)

	// When
	_, err := src.DumpScreen(context.Background(), "0", true)

	// Then
	if !errors.Is(err, ErrPaneGone) {
		t.Fatalf("got %v, want ErrPaneGone", err)
	}
}

func Test_DumpScreen_timeout_maps_typed_error(t *testing.T) {
	// Given: a wedged zellij server (dump blocks until ctx deadline).
	d := &fakeDump{block: true}
	src := testSourceWithDump(t, &fakeZellij{}, d)
	src.screenTimeout = 50 * time.Millisecond

	// When
	start := time.Now()
	_, err := src.DumpScreen(context.Background(), "7", true)

	// Then
	if !errors.Is(err, ErrScreenTimeout) {
		t.Fatalf("got %v, want ErrScreenTimeout", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("DumpScreen hung past the screen timeout budget")
	}
}

func Test_DumpScreen_missing_binary_maps_typed_error(t *testing.T) {
	// Given: a dump failing like exec does for a missing binary.
	d := &fakeDump{err: &exec.Error{Name: "zellij", Err: exec.ErrNotFound}}
	src := testSourceWithDump(t, &fakeZellij{}, d)

	// When
	_, err := src.DumpScreen(context.Background(), "7", true)

	// Then
	if !errors.Is(err, ErrZellijMissing) {
		t.Fatalf("got %v, want ErrZellijMissing", err)
	}
}

func Test_DumpScreen_rejects_empty_pane_id(t *testing.T) {
	// Given
	d := &fakeDump{stdout: []byte("x")}
	src := testSourceWithDump(t, &fakeZellij{}, d)

	// When
	_, err := src.DumpScreen(context.Background(), "  ", true)

	// Then
	if err == nil {
		t.Fatalf("expected error for empty pane id")
	}
	if len(d.calls) != 0 {
		t.Fatalf("empty pane id must never exec, got %d calls", len(d.calls))
	}
}
