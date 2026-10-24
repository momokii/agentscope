package collector

import (
	"context"
	"fmt"
	"testing"

	"opencode-dashboard/internal/model"
)

// BenchmarkTick measures the full join (watermark + Sessions + Enrich +
// DeriveStatus + KPIs + sort + publish) over 100 sessions against the p95
// <1s tick budget on tens of sessions.
func BenchmarkTick(b *testing.B) {
	// Given: 100 generated fixture rows across all statuses
	statuses := []model.SessionStatus{
		model.SessionRunning, model.SessionIdle, model.SessionWaitingInput,
		model.SessionError, model.SessionArchived,
	}
	var rows []model.SessionRow
	for i := 0; i < 100; i++ {
		r := rowFixture(fmt.Sprintf("ses-%03d", i), statuses[i%len(statuses)], int64(i*100), float64(i))
		rows = append(rows, r)
	}
	db := &fakeDB{rows: rows, updMs: 1, evID: 1}
	c := newTestCollector(db, &fakeRuntime{}, &fakeSink{})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.setWatermark(int64(i+1), 1)
		if _, err := c.Tick(ctx); err != nil {
			b.Fatalf("Tick: %v", err)
		}
	}
}
