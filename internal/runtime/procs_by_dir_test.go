package runtime

import (
	"context"
	"testing"

	"opencode-dashboard/internal/model"
)

// Given: two alive opencode processes in /proj/alpha, one zombie there, one
// alive in /proj/beta, and one alive process with an unreadable CWD.
// When: Enrich runs the /proc scan.
// Then: ProcsByDir counts only live processes per cleaned directory and
// omits the unattributable one.
func Test_Enrich_ProcsByDir_counts_live_only(t *testing.T) {
	// Given
	f := &fakeZellij{sessionsOut: "\n", panes: map[string]string{}}
	src := testSource(t, f, nil, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 101, "opencode", "R (running)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 102, "opencode", "Z (zombie)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 200, "opencode", "S (sleeping)", 1, "/proj/beta")
		writeFixtureProc(t, root, 300, "opencode", "S (sleeping)", 1, "")
	})

	// When
	_, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	got := src.ProcsByDir()
	if len(got) != 2 || got["/proj/alpha"] != 2 || got["/proj/beta"] != 1 {
		t.Fatalf("ProcsByDir = %v, want alpha=2 beta=1", got)
	}
}
