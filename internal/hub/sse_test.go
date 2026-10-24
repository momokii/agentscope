package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// fixtureSnapshot builds a contract-valid tick with n sessions covering the
// frozen status enum round-robin, so shape tests exercise real values.
func fixtureSnapshot(seq uint64, n int) model.Snapshot {
	statuses := []model.SessionStatus{
		model.SessionRunning,
		model.SessionIdle,
		model.SessionWaitingInput,
		model.SessionError,
		model.SessionArchived,
	}
	sessions := make([]model.SessionRow, 0, n)
	for i := 0; i < n; i++ {
		row := fixtureSessionRow("ses_fixture_" + string(rune('a'+i)))
		row.Status = statuses[i%len(statuses)]
		sessions = append(sessions, row)
	}
	return model.Snapshot{
		Seq:        seq,
		Sessions:   sessions,
		KPIs:       model.KPIs{Active: 1, WaitingInput: 1, TokensPerHour: 1000, CostToday: 0.5, Errors: 1, SSEConnected: true},
		LastPollMs: 120,
		Stale:      false,
	}
}

// fixtureSessionRow builds a model-valid fleet row for shape tests. Status
// stays inside the frozen enum so UI switches written against model never
// hit an unknown. (Moved here from the deleted fixtures.go stub: only the
// fleet/snapshot shape tests still need a canned row; the detail routes
// now serve the live DetailSource seam.)
func fixtureSessionRow(id string) model.SessionRow {
	return model.SessionRow{
		ID:                id,
		Title:             "Fixture session (fleet shape tests only)",
		Project:           "global",
		Directory:         "/tmp",
		Model:             "fixture/provider",
		Status:            model.SessionIdle,
		Tokens:            model.TokenCounts{In: 100, Out: 50, Total: 150},
		Cost:              0.001,
		UpdatedRelativeMs: 1000,
		Spark:             []float64{0.5, 1.0},
		SubagentCount:     0,
		Pane:              model.PaneInfo{CWD: "/tmp", Exited: false},
		Orphan:            false,
	}
}

// runHub starts h.Run and registers shutdown. It returns the cancel func.
func runHub(t *testing.T, h *Hub) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("hub Run did not stop after cancel")
		}
	})
	return cancel
}

// awaitFrame waits for one frame on ch within the timeout.
func awaitFrame(t *testing.T, ch <-chan frame, timeout time.Duration) frame {
	t.Helper()
	select {
	case fr, ok := <-ch:
		if !ok {
			t.Fatalf("frame channel closed while waiting")
		}
		return fr
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for frame")
		return frame{}
	}
}

// Test_Hub_implements_SnapshotSink locks the frozen seam: Hub must satisfy
// ports.SnapshotSink with the exact Publish signature (compile-time var _
// covers it; this exercises it at runtime).
func Test_Hub_implements_SnapshotSink(t *testing.T) {
	// Given
	var sink ports.SnapshotSink = NewHub()
	ctx := context.Background()

	// When
	err := sink.Publish(ctx, fixtureSnapshot(1, 1))

	// Then
	if err != nil {
		t.Fatalf("Publish returned %v, want nil", err)
	}
}

// Test_Hub_resume_replays_full_snapshot_after_reconnect kills a viewer
// stream, advances ticks, then resubscribes: the new viewer must get the
// latest FULL snapshot (not a delta, not the stale tick).
func Test_Hub_resume_replays_full_snapshot_after_reconnect(t *testing.T) {
	// Given
	h := NewHub(WithTickInterval(20*time.Millisecond), WithViewerQueueCap(8))
	runHub(t, h)
	ctx := context.Background()
	if err := h.Publish(ctx, fixtureSnapshot(1, 2)); err != nil {
		t.Fatalf("Publish seq 1: %v", err)
	}
	sub1, err := h.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	first := awaitFrame(t, sub1.Ch(), 3*time.Second)
	if first.seq != 1 {
		t.Fatalf("first frame seq = %d, want 1", first.seq)
	}

	// When: kill the stream, advance two ticks, reconnect.
	sub1.Unsubscribe()
	if err := h.Publish(ctx, fixtureSnapshot(2, 2)); err != nil {
		t.Fatalf("Publish seq 2: %v", err)
	}
	if err := h.Publish(ctx, fixtureSnapshot(3, 2)); err != nil {
		t.Fatalf("Publish seq 3: %v", err)
	}
	sub2, err := h.Subscribe()
	if err != nil {
		t.Fatalf("resubscribe: %v", err)
	}
	defer sub2.Unsubscribe()

	// Then: reconnect replays the latest full snapshot.
	got := awaitFrame(t, sub2.Ch(), 3*time.Second)
	if got.seq != 3 {
		t.Fatalf("resumed frame seq = %d, want 3 (latest full snapshot)", got.seq)
	}
	var snap model.Snapshot
	if err := json.Unmarshal(got.payload, &snap); err != nil {
		t.Fatalf("frame payload is not a Snapshot: %v", err)
	}
	if snap.Seq != 3 || len(snap.Sessions) != 2 {
		t.Fatalf("resumed snapshot = seq %d with %d sessions, want seq 3 with 2", snap.Seq, len(snap.Sessions))
	}
	for _, row := range snap.Sessions {
		if !row.Status.IsValid() {
			t.Fatalf("resumed session %q has invalid status %q", row.ID, row.Status)
		}
	}
}

// Test_Hub_slow_viewer_dropped_without_stalling_publish floods ticks into
// an unread viewer: every Publish must return promptly, the slow viewer
// must be evicted, and the drop must be counted.
func Test_Hub_slow_viewer_dropped_without_stalling_publish(t *testing.T) {
	// Given
	h := NewHub(WithTickInterval(10*time.Millisecond), WithViewerQueueCap(1))
	runHub(t, h)
	sub, err := h.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	ctx := context.Background()

	// When: publish ticks faster than the viewer reads. Publishes are paced
	// so distinct seqs land on separate fan-out ticks (a tight loop would
	// coalesce into one broadcast — that is aggregation working, not a
	// slow viewer). The viewer never reads, so it must fall behind.
	start := time.Now()
	evicted := false
	for seq := uint64(1); seq <= 200 && !evicted; seq++ {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := h.Publish(pctx, fixtureSnapshot(seq, 1))
		cancel()
		if err != nil {
			t.Fatalf("Publish seq %d blocked or failed: %v", seq, err)
		}
		time.Sleep(15 * time.Millisecond)
		evicted = h.DroppedViewers() > 0
	}
	elapsed := time.Since(start)

	// Then: the poll loop never stalled.
	if elapsed > 5*time.Second {
		t.Fatalf("paced publishes took %v, poll loop stalled", elapsed)
	}

	// Then: the slow viewer was dropped and counted.
	deadline := time.Now().Add(3 * time.Second)
	for h.DroppedViewers() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.DroppedViewers() == 0 {
		t.Fatalf("slow viewer was not evicted (dropped_viewers = 0)")
	}
	if h.DroppedFrames() == 0 {
		t.Fatalf("dropped frames not counted (dropped_frames = 0)")
	}
	if _, ok := <-sub.Ch(); ok {
		// Channel may still hold one queued frame before the close; drain
		// it and require the close to follow.
		if _, ok := <-sub.Ch(); ok {
			t.Fatalf("evicted viewer channel stayed open")
		}
	}

	// Then: the hub still serves after eviction.
	if err := h.Publish(ctx, fixtureSnapshot(101, 1)); err != nil {
		t.Fatalf("Publish after eviction: %v", err)
	}
	latest, ok := h.Latest()
	if !ok || latest.Seq != 101 {
		t.Fatalf("Latest after eviction = %+v, want seq 101", latest)
	}
}

// Test_Hub_shutdown_drains_viewers verifies context-cancel shutdown closes
// every viewer channel and reports zero viewers (no leaked goroutines per
// viewer — the Run goroutine itself returns before this asserts).
func Test_Hub_shutdown_drains_viewers(t *testing.T) {
	// Given
	h := NewHub(WithTickInterval(10 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.Run(ctx)
	}()
	sub1, err := h.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	sub2, err := h.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe 2: %v", err)
	}
	if got := h.ViewerCount(); got != 2 {
		t.Fatalf("ViewerCount = %d, want 2", got)
	}

	// When
	cancel()

	// Then
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("hub Run did not return after cancel")
	}
	deadline := time.Now().Add(3 * time.Second)
	for h.ViewerCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := h.ViewerCount(); got != 0 {
		t.Fatalf("ViewerCount after shutdown = %d, want 0", got)
	}
	for i, sub := range []*Subscription{sub1, sub2} {
		if _, ok := <-sub.Ch(); ok {
			t.Fatalf("viewer %d channel open after shutdown", i+1)
		}
	}
	if err := h.Publish(context.Background(), fixtureSnapshot(9, 1)); err == nil {
		t.Fatalf("Publish after shutdown succeeded, want ErrHubClosed")
	}
}

// Test_Hub_production_defaults_in_contract_windows asserts the shipped
// cadence sits inside 250–1000ms ticks and 15–30s keepalives.
func Test_Hub_production_defaults_in_contract_windows(t *testing.T) {
	// Given / When
	h := NewHub()

	// Then
	if got := h.TickInterval(); got < 250*time.Millisecond || got > 1000*time.Millisecond {
		t.Fatalf("default tick %v outside 250–1000ms contract window", got)
	}
	if got := h.KeepaliveInterval(); got < 15*time.Second || got > 30*time.Second {
		t.Fatalf("default keepalive %v outside 15–30s contract window", got)
	}
}
