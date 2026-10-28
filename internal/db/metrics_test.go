package db

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

// Given: message rows inside the last minute, earlier today, yesterday, a
// user turn with no tokens, and one corrupt blob.
// When: MessageMetrics runs at noon in UTC.
// Then: tokens are the trailing-60s message total and cost is everything
// since local midnight, with corrupt rows contributing zero.
func Test_MessageMetrics_counts_trailing_minute_and_midnight(t *testing.T) {
	// Given
	db := openFixture(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	seed := func(id string, at time.Time, role string, tokens int64, cost float64) {
		data := fmt.Sprintf(`{"role":%q,"tokens":{"total":%d},"cost":%g}`, role, tokens, cost)
		seedMessage(t, db, id, "ses_metrics", at.UnixMilli(), data)
	}
	seed("m_now", now.Add(-30*time.Second), "assistant", 100, 1.5)
	seed("m_90s", now.Add(-90*time.Second), "assistant", 200, 2.5)
	seed("m_midnight", midnight, "assistant", 300, 4)
	seed("m_yesterday", midnight.Add(-time.Second), "assistant", 400, 8)
	seed("m_user", now.Add(-10*time.Second), "user", 0, 0)
	seedMessage(t, db, "m_corrupt", "ses_metrics", now.Add(-10*time.Second).UnixMilli(), "not-json")
	r := fixtureReader(db)

	// When
	got, err := r.MessageMetrics(context.Background(), now)

	// Then
	if err != nil {
		t.Fatalf("MessageMetrics: %v", err)
	}
	if got.TokensLast60s != 100 {
		t.Errorf("TokensLast60s = %d, want 100", got.TokensLast60s)
	}
	if math.Abs(got.CostSinceMidnight-8.0) > 1e-9 {
		t.Errorf("CostSinceMidnight = %v, want 8.0", got.CostSinceMidnight)
	}
}

// Given: message rows across 30 days in two sessions (one corrupt blob).
// When: WindowMetrics runs at noon in UTC.
// Then: global 24h/7d/30d sums and the sparse per-session map match,
// with corrupt rows contributing zero.
func Test_WindowMetrics_counts_windows_globally_and_per_session(t *testing.T) {
	// Given
	db := openFixture(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	seed := func(id, ses string, at time.Time, tokens int64, cost float64) {
		data := fmt.Sprintf(`{"role":"assistant","tokens":{"total":%d},"cost":%g}`, tokens, cost)
		seedMessage(t, db, id, ses, at.UnixMilli(), data)
	}
	seed("w_now", "ses_a", now.Add(-time.Hour), 100, 1.0)
	seed("w_2d", "ses_a", now.Add(-48*time.Hour), 200, 2.0)
	seed("w_10d", "ses_b", now.Add(-240*time.Hour), 300, 3.0)
	seed("w_40d", "ses_b", now.Add(-960*time.Hour), 400, 4.0)
	seedMessage(t, db, "w_corrupt", "ses_a", now.Add(-time.Hour).UnixMilli(), "not-json")
	r := fixtureReader(db)

	// When
	got, err := r.WindowMetrics(context.Background(), now)

	// Then
	if err != nil {
		t.Fatalf("WindowMetrics: %v", err)
	}
	if got.Tokens24h != 100 || math.Abs(got.Cost24h-1.0) > 1e-9 {
		t.Errorf("24h = %d/%v, want 100/1.0", got.Tokens24h, got.Cost24h)
	}
	if got.Tokens7d != 300 || math.Abs(got.Cost7d-3.0) > 1e-9 {
		t.Errorf("7d = %d/%v, want 300/3.0", got.Tokens7d, got.Cost7d)
	}
	if got.Tokens30d != 600 || math.Abs(got.Cost30d-6.0) > 1e-9 {
		t.Errorf("30d = %d/%v, want 600/6.0", got.Tokens30d, got.Cost30d)
	}
	if math.Abs(got.CostMidnight-1.0) > 1e-9 {
		t.Errorf("CostMidnight = %v, want 1.0", got.CostMidnight)
	}
	if len(got.BySession) != 2 {
		t.Fatalf("BySession sessions = %d, want 2 (sparse)", len(got.BySession))
	}
	if got.BySession["ses_a"].Cost24h != 1.0 || got.BySession["ses_b"].Cost24h != 0 {
		t.Errorf("BySession 24h costs = %+v", got.BySession)
	}
}

// Given: an empty message table.
// When: MessageMetrics runs.
// Then: both values are zero, not an error.
func Test_MessageMetrics_empty_table_is_zero(t *testing.T) {
	// Given
	db := openFixture(t)
	r := fixtureReader(db)

	// When
	got, err := r.MessageMetrics(context.Background(), time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))

	// Then
	if err != nil {
		t.Fatalf("MessageMetrics: %v", err)
	}
	if got.TokensLast60s != 0 || got.CostSinceMidnight != 0 {
		t.Fatalf("empty metrics = %+v, want zeros", got)
	}
}
