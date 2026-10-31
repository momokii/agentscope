package main

import (
	"context"
	"errors"
	"testing"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
)

// fakeExportSource stands in for the db and pi readers: canned payload or
// canned error per id.
type fakeExportSource struct {
	payload model.SessionExport
	err     error
	calls   int
}

func (f *fakeExportSource) SessionExport(_ context.Context, _ string) (model.SessionExport, error) {
	f.calls++
	return f.payload, f.err
}

func hitPayload(id string) model.SessionExport {
	return model.SessionExport{Session: model.ExportSession{Info: model.ExportSessionInfo{ID: id}}}
}

// The mux serves db hits directly and never consults pi for them.
func Test_ExportMux_db_hit_skips_pi(t *testing.T) {
	dbsrc := &fakeExportSource{payload: hitPayload("opencode-1")}
	pisrc := &fakeExportSource{err: db.ErrNotFound}
	mux := exportMux{db: dbsrc, pi: pisrc}
	got, err := mux.SessionExport(context.Background(), "opencode-1")
	if err != nil {
		t.Fatalf("mux: %v", err)
	}
	if got.Session.Info.ID != "opencode-1" {
		t.Errorf("id = %q", got.Session.Info.ID)
	}
	if pisrc.calls != 0 {
		t.Errorf("pi consulted on db hit (%d calls)", pisrc.calls)
	}
}

// Unknown to the db falls through to pi (P6); pi misses stay 404-shaped.
func Test_ExportMux_db_miss_falls_through_to_pi(t *testing.T) {
	dbsrc := &fakeExportSource{err: db.ErrNotFound}
	pisrc := &fakeExportSource{payload: hitPayload("pi-1")}
	mux := exportMux{db: dbsrc, pi: pisrc}
	got, err := mux.SessionExport(context.Background(), "pi-1")
	if err != nil {
		t.Fatalf("mux: %v", err)
	}
	if got.Session.Info.ID != "pi-1" {
		t.Errorf("id = %q", got.Session.Info.ID)
	}
	if dbsrc.calls != 1 || pisrc.calls != 1 {
		t.Errorf("calls db=%d pi=%d, want 1/1", dbsrc.calls, pisrc.calls)
	}
}

// Genuine db failures short-circuit: pi is never a fallback for errors.
func Test_ExportMux_db_error_does_not_consult_pi(t *testing.T) {
	boom := errors.New("wal locked")
	dbsrc := &fakeExportSource{err: boom}
	pisrc := &fakeExportSource{payload: hitPayload("pi-1")}
	mux := exportMux{db: dbsrc, pi: pisrc}
	_, err := mux.SessionExport(context.Background(), "pi-1")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the db failure", err)
	}
	if pisrc.calls != 0 {
		t.Errorf("pi consulted on db error (%d calls)", pisrc.calls)
	}
}

// Without pi configured the mux is a pure passthrough (opencode-only hosts
// behave exactly as before P6).
func Test_ExportMux_nil_pi_passthrough(t *testing.T) {
	dbsrc := &fakeExportSource{err: db.ErrNotFound}
	mux := exportMux{db: dbsrc, pi: nil}
	_, err := mux.SessionExport(context.Background(), "ghost")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
