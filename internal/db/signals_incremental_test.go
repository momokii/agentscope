package db

import (
	"context"
	"testing"
)

// Given: a corpus of part blobs spanning every verdict class.
// When: the running-tool prefilter is consulted.
// Then: it returns true for every blob that COULD decode to running
// (including prose false-positives and corrupt blobs, which still take the
// full parse), and false only for blobs that provably cannot be running.
// Reverting the fix (deleting the helper) fails this file to compile.
func Test_MaybeRunningTool_prefilter_exact(t *testing.T) {
	// Given
	cases := []struct {
		name string
		blob string
		want bool
	}{
		{"running tool", `{"type":"tool","tool":"bash","state":{"status":"running"}}`, true},
		{"completed tool", `{"type":"tool","tool":"bash","state":{"status":"completed"}}`, false},
		{"pending tool", `{"type":"tool","state":{"status":"pending"}}`, false},
		{"text part", `{"type":"text","text":"hello"}`, false},
		// Prose mentioning running carries no QUOTED substring, so the
		// prefilter skips it — exactly, since a text part (or a completed
		// tool) cannot decode to running no matter what its prose says.
		{"prose mentions running", `{"type":"text","text":"still running late"}`, false},
		{"completed output mentions running", `{"type":"tool","state":{"status":"completed"},"output":"job running done"}`, false},
		// A stray "running" KEY is the true false positive: prefilter says
		// "maybe", the full parse below still clears it.
		{"stray running key", `{"type":"text","running":true}`, true},
		{"corrupt with substring", `{"type":"tool","running":`, true},
		{"corrupt without substring", `{"type":"tool",pending`, false},
		{"empty", ``, false},
	}

	// When/Then
	for _, tc := range cases {
		if got := maybeRunningTool(tc.blob); got != tc.want {
			t.Errorf("%s: maybeRunningTool = %v, want %v", tc.name, got, tc.want)
		}
		if !maybeRunningTool(tc.blob) {
			// Contract: a false prefilter proves a non-running verdict —
			// the skipped parse could not have counted the blob.
			part, ok := decodePartPayload(tc.blob)
			if ok && part.Type == "tool" && part.State != nil && part.State.Status == "running" {
				t.Errorf("%s: prefilter false but blob decodes to running", tc.name)
			}
		}
	}
}

// Given: a fixture event tail with duplicates, fresh flags, and eviction.
// When: the incremental pass seeds, advances through inserts (new keys,
// duplicate keys, backfills, a new aggregate, eviction flipping a flag),
// and reseeds after the table regresses.
// Then: the seed tick reports no fresh events (delta-only: history is not
// news); every delta tick marks exactly the touched aggregates whose merged
// tail holds a fresh row, and the tail MERGE still equals the stateless full
// scan on the same rows. Reverting the fix fails this file to compile
// (incremental entry points vanish) and any merge skew fails the equalities.
func Test_BatchEventsIncremental_matches_fullscan(t *testing.T) {
	// Given
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_a", projectID: "global", title: "a",
		directory: "/tmp", updatedAgo: 1000})
	seedSession(t, db, sessionSeed{id: "ses_b", projectID: "global", title: "b",
		directory: "/tmp", updatedAgo: 1000})
	// ses_a: seqs 1..12, newest fresh; duplicate seq 5 (stale first wins).
	for seq := int64(1); seq <= 11; seq++ {
		seedEvent(t, db, "ev_a_"+string(rune('0'+seq)), "ses_a", seq, "session.updated.1")
	}
	seedEvent(t, db, "ev_a_dup_stale", "ses_a", 5, "session.updated.1")
	seedEvent(t, db, "ev_a_dup_fresh", "ses_a", 5, "message.part.updated.1")
	seedEvent(t, db, "ev_a_12", "ses_a", 12, "message.part.updated.1")
	// ses_b: quiet, no fresh flag.
	for seq := int64(1); seq <= 3; seq++ {
		seedEvent(t, db, "ev_b_"+string(rune('0'+seq)), "ses_b", seq, "session.updated.1")
	}
	r := fixtureReader(db)
	ctx := context.Background()
	ids := map[string]struct{}{"ses_a": {}, "ses_b": {}}

	// When: tick 1 (seed)
	inc := &fleetSignals{fresh: make(map[string]bool)}
	if err := r.batchEventSignalsIncremental(ctx, inc, ids); err != nil {
		t.Fatalf("incremental tick 1: %v", err)
	}
	// Then: the seed tick reports NO fresh events — pre-existing tail rows
	// are history, not news (delta-only semantics). The tail merge itself
	// still matches the full scan; only the flag derivation differs.
	full := &fleetSignals{fresh: make(map[string]bool)}
	if err := r.batchEventSignals(ctx, full); err != nil {
		t.Fatalf("full scan: %v", err)
	}
	if len(inc.fresh) != 0 {
		t.Fatalf("tick 1 (seed) flags = %v, want empty", inc.fresh)
	}
	if !full.fresh["ses_a"] || full.fresh["ses_b"] {
		t.Fatalf("full scan tick 1 flags = %v, want {ses_a:true}", full.fresh)
	}
	if !r.eventSeeded || r.eventWatermark <= 0 {
		t.Fatalf("seeded=%v watermark=%d, want seeded with positive watermark", r.eventSeeded, r.eventWatermark)
	}
	wm1 := r.eventWatermark

	// When: tick 2 — new fresh key for ses_b, duplicate of a cached key
	// (must lose), backfill below the tail, a brand-new aggregate, and 13
	// non-fresh rows for ses_a evicting its fresh flag.
	seedEvent(t, db, "ev_b_4", "ses_b", 4, "message.part.updated.1")
	seedEvent(t, db, "ev_a_dup_late", "ses_a", 12, "session.updated.1")
	seedEvent(t, db, "ev_a_backfill", "ses_a", 2, "message.part.updated.1")
	seedSession(t, db, sessionSeed{id: "ses_c", projectID: "global", title: "c",
		directory: "/tmp", updatedAgo: 500})
	seedEvent(t, db, "ev_c_1", "ses_c", 1, "message.part.updated.1")
	ids["ses_c"] = struct{}{}
	for seq := int64(13); seq <= 25; seq++ {
		seedEvent(t, db, "ev_a_ev_"+string(rune('a'+seq-13)), "ses_a", seq, "session.updated.1")
	}
	inc2 := &fleetSignals{fresh: make(map[string]bool)}
	if err := r.batchEventSignalsIncremental(ctx, inc2, ids); err != nil {
		t.Fatalf("incremental tick 2: %v", err)
	}
	// Then
	full2 := &fleetSignals{fresh: make(map[string]bool)}
	if err := r.batchEventSignals(ctx, full2); err != nil {
		t.Fatalf("full scan 2: %v", err)
	}
	if !equalFresh(inc2.fresh, full2.fresh) {
		t.Fatalf("tick 2 mismatch: incremental %v vs full %v", inc2.fresh, full2.fresh)
	}
	if inc2.fresh["ses_a"] {
		t.Errorf("tick 2: ses_a flag should have flipped false after eviction, got %v", inc2.fresh)
	}
	if !inc2.fresh["ses_b"] || !inc2.fresh["ses_c"] {
		t.Errorf("tick 2 flags = %v, want ses_b and ses_c fresh", inc2.fresh)
	}
	if r.eventWatermark <= wm1 {
		t.Errorf("watermark did not advance: %d -> %d", wm1, r.eventWatermark)
	}

	// When: tick 3 — the table regresses (restore/swap: MAX(rowid) drops
	// far below the watermark), forcing a reseed. The watermark is bumped by
	// hand to simulate a long-lived process: at fixture scale a
	// delete-and-reinsert reuses rowid 1, which can never drop below
	// watermark-overlap at small watermarks; in production the watermark is
	// ~1M and any swap trips the guard.
	r.eventWatermark += 100000
	if _, err := db.Exec(`DELETE FROM event`); err != nil {
		t.Fatalf("clear events: %v", err)
	}
	seedEvent(t, db, "ev_a_new_1", "ses_a", 1, "session.updated.1")
	inc3 := &fleetSignals{fresh: make(map[string]bool)}
	if err := r.batchEventSignalsIncremental(ctx, inc3, ids); err != nil {
		t.Fatalf("incremental tick 3 (reseed): %v", err)
	}
	// Then
	full3 := &fleetSignals{fresh: make(map[string]bool)}
	if err := r.batchEventSignals(ctx, full3); err != nil {
		t.Fatalf("full scan 3: %v", err)
	}
	if !equalFresh(inc3.fresh, full3.fresh) {
		t.Fatalf("tick 3 mismatch: incremental %v vs full %v", inc3.fresh, full3.fresh)
	}
	if len(inc3.fresh) != 0 {
		t.Errorf("tick 3 flags = %v, want empty after reseed", inc3.fresh)
	}
}

func equalFresh(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
