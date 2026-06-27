package store

import (
	"strings"
	"testing"
	"time"
)

func TestReserveAutoAllocatesLowest(t *testing.T) {
	s := newTestStore(t) // range 20000-20003
	r, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "web"})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if r.Port != 20000 {
		t.Fatalf("expected 20000, got %d", r.Port)
	}
	if !r.ExpiresAt.Equal(s.now().Add(time.Hour)) {
		t.Fatalf("expiry not set to now+TTL: %v", r.ExpiresAt)
	}
	r2, _ := s.Reserve(ReserveRequest{Owner: "b", Purpose: "db"})
	if r2.Port != 20001 {
		t.Fatalf("expected 20001, got %d", r2.Port)
	}
}

func TestReserveSpecificPort(t *testing.T) {
	s := newTestStore(t)
	r, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "pg", Port: 20002})
	if err != nil || r.Port != 20002 {
		t.Fatalf("Reserve specific: port=%d err=%v", r.Port, err)
	}
}

func TestReserveSpecificTakenErrorsWithOwner(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Reserve(ReserveRequest{Owner: "first", Purpose: "x", Port: 20000}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Reserve(ReserveRequest{Owner: "second", Purpose: "y", Port: 20000})
	if err == nil || !strings.Contains(err.Error(), "first") {
		t.Fatalf("expected conflict error naming owner 'first', got %v", err)
	}
}

func TestReserveExhaustionErrors(t *testing.T) {
	s := newTestStore(t) // 4 ports: 20000-20003
	for i := 0; i < 4; i++ {
		if _, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x"}); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	if _, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x"}); err == nil {
		t.Fatal("expected exhaustion error, got nil")
	}
}

func TestReserveRequiresOwner(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Reserve(ReserveRequest{Purpose: "x"}); err == nil {
		t.Fatal("expected error for missing owner")
	}
}
