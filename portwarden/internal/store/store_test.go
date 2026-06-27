package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return New(Config{
		Path:       filepath.Join(dir, "reservations.json"),
		RangeLow:   20000,
		RangeHigh:  20003, // tiny range so exhaustion is testable
		DefaultTTL: time.Hour,
		Now:        func() time.Time { return time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC) },
		PIDAlive:   func(int) bool { return true },
	})
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	st := &state{Reservations: []Reservation{
		{Port: 20000, Owner: "a", Purpose: "x", CreatedAt: s.now(), ExpiresAt: s.now().Add(time.Hour)},
	}}
	if err := saveState(s.cfg.Path, st); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got, err := loadState(s.cfg.Path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(got.Reservations) != 1 || got.Reservations[0].Port != 20000 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s := newTestStore(t)
	got, err := loadState(s.cfg.Path)
	if err != nil {
		t.Fatalf("loadState on missing file should not error: %v", err)
	}
	if len(got.Reservations) != 0 {
		t.Fatalf("expected empty state, got %+v", got)
	}
}

func TestLoadCorruptFileErrors(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.cfg.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.cfg.Path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(s.cfg.Path); err == nil {
		t.Fatal("expected error on corrupt state file, got nil")
	}
}
