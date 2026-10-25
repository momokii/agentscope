package db

import (
	"context"
	"testing"

	"opencode-dashboard/internal/model"
)

const (
	dayMs         = int64(24 * 3600 * 1000)
	ancientAgoMs  = 73 * dayMs
	ancientMsgMs  = fixedNowUnixMs - 73*dayMs
	recentMsgMs   = fixedNowUnixMs - 60*1000
	completedMsg  = `{"role":"assistant","time":{"created":1,"completed":2},"providerID":"opencode","modelID":"m1"}`
	abortedMsg    = `{"role":"assistant","time":{"created":3},"providerID":"opencode","modelID":"m1"}`
	ancientUser   = `{"role":"user","time":{"created":5}}`
	recentToolRun = `{"type":"tool","tool":"bash","state":{"status":"running"}}`
)

// fixedNowUnixMs pins the fixture clock for message timestamps.
const fixedNowUnixMs = int64(1800000000000)

// Test_Sessions_recency_gate proves the status-truth contract end to end:
// DeriveStatus returns running/waiting-input/error only for sessions whose
// time_updated is inside the active window; ancient sessions derive idle no
// matter how stale their signal rows look. Archived still wins.
func Test_Sessions_recency_gate(t *testing.T) {
	// Given: one recent and one ancient session per signal class
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	recent := func(id string) sessionSeed {
		return sessionSeed{id: id, projectID: "global", title: id,
			directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
			updatedAgo: 5000}
	}
	ancient := func(id string) sessionSeed {
		return sessionSeed{id: id, projectID: "global", title: id,
			directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
			updatedAgo: ancientAgoMs}
	}
	seedSession(t, db, recent("ses_recent_inflight"))
	seedMessage(t, db, "m_recent_inflight", "ses_recent_inflight", recentMsgMs, abortedMsg)
	seedSession(t, db, ancient("ses_ancient_inflight"))
	seedMessage(t, db, "m_ancient_inflight", "ses_ancient_inflight", recentMsgMs, abortedMsg)
	seedSession(t, db, recent("ses_recent_tool"))
	seedMessage(t, db, "m_recent_tool", "ses_recent_tool", 1, completedMsg)
	seedPart(t, db, "p_recent_tool", "m_recent_tool", "ses_recent_tool", 2, recentToolRun)
	seedSession(t, db, ancient("ses_ancient_tool"))
	seedMessage(t, db, "m_ancient_tool", "ses_ancient_tool", 1, completedMsg)
	seedPart(t, db, "p_ancient_tool", "m_ancient_tool", "ses_ancient_tool", 2, recentToolRun)
	seedSession(t, db, recent("ses_recent_user"))
	seedMessage(t, db, "m_recent_user", "ses_recent_user", recentMsgMs, ancientUser)
	seedSession(t, db, ancient("ses_ancient_user"))
	seedMessage(t, db, "m_ancient_user", "ses_ancient_user", recentMsgMs, ancientUser)
	seedSession(t, db, recent("ses_recent_error"))
	seedMessage(t, db, "m_recent_error", "ses_recent_error", 1, errorMsg)
	seedSession(t, db, ancient("ses_ancient_error"))
	seedMessage(t, db, "m_ancient_error", "ses_ancient_error", 1, errorMsg)
	arch := ancient("ses_ancient_archived")
	arch.archived = true
	seedSession(t, db, arch)
	seedMessage(t, db, "m_ancient_archived", "ses_ancient_archived", recentMsgMs, abortedMsg)
	r := fixtureReader(db)

	// When
	rows, err := r.Sessions(context.Background())

	// Then
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	want := map[string]model.SessionStatus{
		"ses_recent_inflight":  model.SessionRunning,
		"ses_ancient_inflight": model.SessionIdle,
		"ses_recent_tool":      model.SessionRunning,
		"ses_ancient_tool":     model.SessionIdle,
		"ses_recent_user":      model.SessionWaitingInput,
		"ses_ancient_user":     model.SessionIdle,
		"ses_recent_error":     model.SessionError,
		"ses_ancient_error":    model.SessionIdle,
		"ses_ancient_archived": model.SessionArchived,
	}
	for id, status := range want {
		if got := findRow(t, rows, id); got.Status != status {
			t.Errorf("%s status = %q, want %q", id, got.Status, status)
		}
	}
}

// Test_Sessions_inflight_requires_recent_message proves an ancient aborted
// assistant message (completed never set, e.g. a killed TUI) cannot mark a
// RECENT session running on its own: the in-flight signal itself is gated on
// the message's own time_created.
func Test_Sessions_inflight_requires_recent_message(t *testing.T) {
	// Given: a recent session whose only assistant message is ancient and
	// aborted (73 days old, no completed field — the live shape)
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_aborted", projectID: "global", title: "a",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
		updatedAgo: 5000})
	seedMessage(t, db, "m_aborted", "ses_aborted", ancientMsgMs, abortedMsg)
	seedSession(t, db, sessionSeed{id: "ses_live", projectID: "global", title: "l",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
		updatedAgo: 5000})
	seedMessage(t, db, "m_live", "ses_live", recentMsgMs, abortedMsg)
	r := fixtureReader(db)

	// When
	rows, err := r.Sessions(context.Background())

	// Then: the ancient aborted turn is stale, the recent one runs
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if got := findRow(t, rows, "ses_aborted"); got.Status != model.SessionIdle {
		t.Errorf("aborted status = %q, want idle", got.Status)
	}
	if got := findRow(t, rows, "ses_live"); got.Status != model.SessionRunning {
		t.Errorf("live status = %q, want running", got.Status)
	}
}

// Test_Sessions_subagent_completed_child_is_idle proves child sessions
// derive identically: a finished subagent (completed turn, no stale
// signals counted through recency) shows its true idle status, whether the
// family is recent or ancient.
func Test_Sessions_subagent_completed_child_is_idle(t *testing.T) {
	// Given: recent and ancient parent/child families, children completed
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_p", projectID: "global", title: "p",
		directory: "/tmp", updatedAgo: 5000})
	seedSession(t, db, sessionSeed{id: "ses_c", projectID: "global", parentID: "ses_p",
		title: "c", directory: "/tmp", updatedAgo: 4000})
	seedMessage(t, db, "m_c", "ses_c", recentMsgMs, completedMsg)
	seedSession(t, db, sessionSeed{id: "ses_pa", projectID: "global", title: "pa",
		directory: "/tmp", updatedAgo: ancientAgoMs})
	seedSession(t, db, sessionSeed{id: "ses_ca", projectID: "global", parentID: "ses_pa",
		title: "ca", directory: "/tmp", updatedAgo: ancientAgoMs})
	seedMessage(t, db, "m_ca", "ses_ca", ancientMsgMs, completedMsg)
	r := fixtureReader(db)

	// When
	rows, err := r.Sessions(context.Background())

	// Then: finished children are idle in both families
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	for _, id := range []string{"ses_c", "ses_ca"} {
		if got := findRow(t, rows, id); got.Status != model.SessionIdle {
			t.Errorf("child %s status = %q, want idle", id, got.Status)
		}
	}
	if got := findRow(t, rows, "ses_p"); got.SubagentCount != 1 {
		t.Errorf("parent subagents = %d, want 1", got.SubagentCount)
	}
}

// Test_Sessions_fresh_events_delta_only proves HasFreshEvents means new
// event rows arrived since the previous tick: the seed tick and a quiet
// delta tick mark nothing, and a delta tick marks only the touched
// aggregate — never its quiet sibling.
func Test_Sessions_fresh_events_delta_only(t *testing.T) {
	// Given: two recent sessions sharing a stale fresh tail
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	for _, id := range []string{"ses_a", "ses_b"} {
		seedSession(t, db, sessionSeed{id: id, projectID: "global", title: id,
			directory: "/tmp", updatedAgo: 1000})
		seedMessage(t, db, "m_"+id, id, 1, completedMsg)
		seedEvent(t, db, "ev_"+id+"_1", id, 1, "message.part.updated.1")
	}
	r := fixtureReader(db)
	ctx := context.Background()

	// When: the seed tick runs
	rows, err := r.Sessions(ctx)
	if err != nil {
		t.Fatalf("seed Sessions: %v", err)
	}

	// Then: no fresh events on the seed tick
	for _, id := range []string{"ses_a", "ses_b"} {
		if got := findRow(t, rows, id); got.Status != model.SessionIdle {
			t.Errorf("seed %s status = %q, want idle", id, got.Status)
		}
	}

	// When: a quiet delta tick runs (no new rows anywhere)
	rows, err = r.Sessions(ctx)
	if err != nil {
		t.Fatalf("quiet Sessions: %v", err)
	}

	// Then: overlap replays must not mark anything fresh
	for _, id := range []string{"ses_a", "ses_b"} {
		if got := findRow(t, rows, id); got.Status != model.SessionIdle {
			t.Errorf("quiet %s status = %q, want idle", id, got.Status)
		}
	}

	// When: new fresh rows arrive for ses_a only
	seedEvent(t, db, "ev_a_2", "ses_a", 2, "message.part.updated.1")
	rows, err = r.Sessions(ctx)
	if err != nil {
		t.Fatalf("delta Sessions: %v", err)
	}

	// Then: only the touched aggregate runs
	if got := findRow(t, rows, "ses_a"); got.Status != model.SessionRunning {
		t.Errorf("touched ses_a status = %q, want running", got.Status)
	}
	if got := findRow(t, rows, "ses_b"); got.Status != model.SessionIdle {
		t.Errorf("untouched ses_b status = %q, want idle", got.Status)
	}
}

// Test_SessionDetail_recency_agrees_with_fleet proves the on-demand
// drill-down derives identically: an ancient session with a stale running
// tool and a stale fresh tail reports idle, matching its fleet row.
func Test_SessionDetail_recency_agrees_with_fleet(t *testing.T) {
	// Given: an ancient session with every stale running signal set
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_old", projectID: "global", title: "o",
		directory: "/tmp", updatedAgo: ancientAgoMs})
	seedMessage(t, db, "m_old", "ses_old", ancientMsgMs, abortedMsg)
	seedPart(t, db, "p_old", "m_old", "ses_old", 2, recentToolRun)
	seedEvent(t, db, "ev_old", "ses_old", 1, "message.part.updated.1")
	r := fixtureReader(db)
	ctx := context.Background()

	// When
	rows, err := r.Sessions(ctx)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	got, err := r.SessionDetail(ctx, "ses_old")
	if err != nil {
		t.Fatalf("SessionDetail: %v", err)
	}

	// Then: drill-down and fleet agree on idle
	if fleet := findRow(t, rows, "ses_old"); fleet.Status != model.SessionIdle {
		t.Errorf("fleet status = %q, want idle", fleet.Status)
	}
	if got.Session.Status != model.SessionIdle {
		t.Errorf("detail status = %q, want idle", got.Session.Status)
	}
}
