package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeFixtureProc builds one fake /proc/<pid> entry: cmdline argv[0],
// status with State + PPid, and an optional cwd symlink.
func writeFixtureProc(t *testing.T, root string, pid int, argv0, state string, ppid int, cwd string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), append([]byte(argv0), 0), 0o644); err != nil {
		t.Fatal(err)
	}
	status := fmt.Sprintf("Name:\t%s\nState:\t%s\nPPid:\t%d\n", argv0, state, ppid)
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644); err != nil {
		t.Fatal(err)
	}
	if cwd != "" {
		if err := os.Symlink(cwd, filepath.Join(dir, "cwd")); err != nil {
			t.Fatal(err)
		}
	}
}

func Test_ClassifyState(t *testing.T) {
	tests := []struct {
		state byte
		want  Liveness
	}{
		{'R', LivenessAlive},
		{'S', LivenessAlive},
		{'D', LivenessAlive},
		{'T', LivenessAlive},
		{'Z', LivenessZombie},
		{'X', LivenessZombie},
		{'x', LivenessZombie},
	}
	for _, tt := range tests {
		t.Run(string([]byte{tt.state}), func(t *testing.T) {
			// When
			got := ClassifyState(tt.state)

			// Then
			if got != tt.want {
				t.Errorf("ClassifyState(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func Test_ReadProc_fields_from_fixture_tree(t *testing.T) {
	// Given
	root := t.TempDir()
	cwd := t.TempDir()
	writeFixtureProc(t, root, 4242, "opencode", "S (sleeping)", 1000, cwd)

	// When / Then
	state, err := ReadProcState(root, 4242)
	if err != nil || state != 'S' {
		t.Errorf("state = %q, %v; want 'S', nil", state, err)
	}
	gotCWD, err := ReadProcCWD(root, 4242)
	if err != nil || gotCWD != cwd {
		t.Errorf("cwd = %q, %v; want %q, nil", gotCWD, err, cwd)
	}
	ppid, err := ReadProcPPID(root, 4242)
	if err != nil || ppid != 1000 {
		t.Errorf("ppid = %d, %v; want 1000, nil", ppid, err)
	}
	if PIDLiveness(root, 4242) != LivenessAlive {
		t.Errorf("pid 4242 must read alive")
	}
}

func Test_ReadProc_missing_pid_reports_exited(t *testing.T) {
	// Given: empty fixture tree (pid 9999 absent).
	root := t.TempDir()

	// When / Then
	if _, err := ReadProcState(root, 9999); !errors.Is(err, ErrProcGone) {
		t.Errorf("state err = %v, want ErrProcGone", err)
	}
	if _, err := ReadProcCWD(root, 9999); !errors.Is(err, ErrProcGone) {
		t.Errorf("cwd err = %v, want ErrProcGone", err)
	}
	if _, err := ReadProcPPID(root, 9999); !errors.Is(err, ErrProcGone) {
		t.Errorf("ppid err = %v, want ErrProcGone", err)
	}
	if PIDLiveness(root, 9999) != LivenessGone {
		t.Errorf("missing pid must read gone")
	}
}

func Test_PIDLiveness_zombie_counts_dead(t *testing.T) {
	// Given
	root := t.TempDir()
	writeFixtureProc(t, root, 777, "opencode", "Z (zombie)", 1000, "")

	// When
	got := PIDLiveness(root, 777)

	// Then
	if got != LivenessZombie {
		t.Errorf("got %q, want zombie", got)
	}
}

func Test_ScanOpencode_matches_argv0_basename_only(t *testing.T) {
	// Given: two opencode procs (one zombie), a shell, and a node child.
	root := t.TempDir()
	writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 10, "/proj/alpha")
	writeFixtureProc(t, root, 101, "/usr/bin/opencode", "Z (zombie)", 11, "/proj/beta")
	writeFixtureProc(t, root, 102, "zsh", "S (sleeping)", 1, "/proj/alpha")
	writeFixtureProc(t, root, 103, "node", "S (sleeping)", 100, "/proj/alpha")

	// When
	found, err := ScanOpencode(root)

	// Then
	if err != nil {
		t.Fatalf("ScanOpencode: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("got %+v, want 2 opencode procs", found)
	}
	byPID := map[int]ProcInfo{}
	for _, p := range found {
		byPID[p.PID] = p
	}
	if !byPID[100].Alive() {
		t.Errorf("pid 100 (S) must be alive")
	}
	if byPID[101].Alive() {
		t.Errorf("pid 101 (Z) must read dead")
	}
	if byPID[100].PPID != 10 || byPID[100].CWD != "/proj/alpha" {
		t.Errorf("pid 100 fields wrong: %+v", byPID[100])
	}
}

func Test_ScanOpencode_unreadable_root_fails(t *testing.T) {
	// When
	_, err := ScanOpencode("/nonexistent-proc-root-xyz")

	// Then
	if err == nil {
		t.Fatal("expected error for unreadable proc root")
	}
}

func Test_ChildPID_resolves_pane_shell_chain(t *testing.T) {
	// Given: shell pid 10 parents opencode pid 100; pid 101 is unrelated.
	root := t.TempDir()
	writeFixtureProc(t, root, 10, "zsh", "S (sleeping)", 1, "/proj/alpha")
	writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 10, "/proj/alpha")
	writeFixtureProc(t, root, 101, "opencode", "S (sleeping)", 99, "/proj/other")

	// When
	got, err := ChildPID(root, 10, "opencode")

	// Then
	if err != nil {
		t.Fatalf("ChildPID: %v", err)
	}
	if got != 100 {
		t.Errorf("got %d, want 100", got)
	}
}

func Test_ChildPID_no_match_reports_typed_error(t *testing.T) {
	// Given
	root := t.TempDir()
	writeFixtureProc(t, root, 10, "zsh", "S (sleeping)", 1, "/proj/alpha")

	// When
	_, err := ChildPID(root, 10, "opencode")

	// Then
	if !errors.Is(err, ErrNoChild) {
		t.Errorf("got %v, want ErrNoChild", err)
	}
}
