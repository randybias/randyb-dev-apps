package store

import (
	"testing"
	"time"
)

func TestRenewExtendsLease(t *testing.T) {
	s := newTestStore(t)
	r, _ := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 20000, TTL: time.Minute})
	want := s.now().Add(2 * time.Hour)
	got, err := s.Renew(20000, 2*time.Hour)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !got.ExpiresAt.Equal(want) {
		t.Fatalf("expiry = %v, want %v", got.ExpiresAt, want)
	}
	_ = r
}

func TestRenewUnknownPortErrors(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Renew(20000, time.Hour); err == nil {
		t.Fatal("expected error renewing unknown port")
	}
}

func TestReleaseRemoves(t *testing.T) {
	s := newTestStore(t)
	s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 20000})
	if err := s.Release(20000); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got, _ := s.List("")
	if len(got) != 0 {
		t.Fatalf("expected empty after release, got %+v", got)
	}
}

func TestReleaseUnknownPortErrors(t *testing.T) {
	s := newTestStore(t)
	if err := s.Release(20000); err == nil {
		t.Fatal("expected error releasing unknown port")
	}
}

func TestListFiltersByOwner(t *testing.T) {
	s := newTestStore(t)
	s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 20000})
	s.Reserve(ReserveRequest{Owner: "b", Purpose: "y", Port: 20001})
	got, err := s.List("a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Owner != "a" {
		t.Fatalf("expected only owner a, got %+v", got)
	}
}

func TestReap(t *testing.T) {
	s := newTestStore(t)
	// Seed state directly: Reserve sweeps on every call, so an expired entry
	// added via Reserve would be reaped by the next Reserve before Reap runs.
	seed := &state{Reservations: []Reservation{
		{Port: 20000, Owner: "old", Purpose: "x", CreatedAt: s.now().Add(-2 * time.Hour), ExpiresAt: s.now().Add(-time.Minute)},
		{Port: 20001, Owner: "new", Purpose: "y", CreatedAt: s.now(), ExpiresAt: s.now().Add(time.Hour)},
	}}
	if err := saveState(s.cfg.Path, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	reclaimed, err := s.Reap()
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].Owner != "old" {
		t.Fatalf("expected to reclaim 'old', got %+v", reclaimed)
	}
	got, _ := s.List("")
	if len(got) != 1 || got[0].Owner != "new" {
		t.Fatalf("expected only 'new' to remain, got %+v", got)
	}
}
