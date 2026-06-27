package store

import (
	"testing"
	"time"
)

func TestSweepRemovesExpiredAndDeadPID(t *testing.T) {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	st := &state{Reservations: []Reservation{
		{Port: 1, Owner: "live", ExpiresAt: now.Add(time.Hour)},               // keep: active
		{Port: 2, Owner: "expired", ExpiresAt: now.Add(-time.Minute)},         // drop: expired
		{Port: 3, Owner: "deadpid", PID: 999, ExpiresAt: now.Add(time.Hour)},  // drop: dead pid
		{Port: 4, Owner: "livepid", PID: 1000, ExpiresAt: now.Add(time.Hour)}, // keep: live pid
	}}
	alive := func(pid int) bool { return pid == 1000 }

	removed := sweep(st, now, alive)

	if len(st.Reservations) != 2 {
		t.Fatalf("expected 2 kept, got %d: %+v", len(st.Reservations), st.Reservations)
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
	for _, r := range st.Reservations {
		if r.Port != 1 && r.Port != 4 {
			t.Fatalf("unexpected survivor: port %d", r.Port)
		}
	}
}

func TestSweepKeepsZeroPIDWithinLease(t *testing.T) {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	st := &state{Reservations: []Reservation{
		{Port: 5, Owner: "nopid", PID: 0, ExpiresAt: now.Add(time.Hour)},
	}}
	removed := sweep(st, now, func(int) bool { return false })
	if len(removed) != 0 || len(st.Reservations) != 1 {
		t.Fatalf("PID 0 within lease must be kept; removed=%d kept=%d", len(removed), len(st.Reservations))
	}
}
