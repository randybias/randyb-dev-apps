package store

import (
	"net"
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

// A port absent from the ledger but physically bound (a process that outlived
// its lease, or never reserved one) must not be handed out.
func TestReserveAutoAllocateSkipsPortsInUse(t *testing.T) {
	s := newTestStoreWithInUse(t, 20000, 20001)
	r, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "web"})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if r.Port != 20002 {
		t.Fatalf("expected 20002 (20000-20001 occupied), got %d", r.Port)
	}
}

func TestReserveAutoAllocateExhaustsWhenAllInUse(t *testing.T) {
	s := newTestStoreWithInUse(t, 20000, 20001, 20002, 20003)
	if _, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x"}); err == nil {
		t.Fatal("expected exhaustion error when every port is occupied")
	}
}

func TestReserveSpecificPortInUseErrors(t *testing.T) {
	s := newTestStoreWithInUse(t, 20002)
	_, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "pg", Port: 20002})
	if err == nil {
		t.Fatal("expected error for a port already bound by a process")
	}
	if !strings.Contains(err.Error(), "in use") {
		t.Fatalf("error should say the port is in use, got %v", err)
	}
}

// Force exists to reclaim a lane whose lease was swept while the process kept
// running -- the recovery path for an occupied-but-unreserved port.
func TestReserveSpecificPortInUseForceSucceeds(t *testing.T) {
	s := newTestStoreWithInUse(t, 20002)
	r, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "pg", Port: 20002, Force: true})
	if err != nil {
		t.Fatalf("Force should bypass the in-use check: %v", err)
	}
	if r.Port != 20002 {
		t.Fatalf("expected 20002, got %d", r.Port)
	}
}

// Force bypasses the process check, never another session's active lease.
func TestReserveForceStillRespectsLedger(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Reserve(ReserveRequest{Owner: "first", Purpose: "x", Port: 20000}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Reserve(ReserveRequest{Owner: "second", Purpose: "y", Port: 20000, Force: true})
	if err == nil || !strings.Contains(err.Error(), "first") {
		t.Fatalf("Force must not override an existing reservation, got %v", err)
	}
}

// A probe that cannot run is not evidence the port is usable. An out-of-range
// port fails net.Listen with a malformed-address error rather than EADDRINUSE,
// which must not be read as "free".
func TestPortFreeRejectsOutOfRangePort(t *testing.T) {
	for _, p := range []int{0, -1, 70000, 99999} {
		if portFree(p) {
			t.Fatalf("port %d is not a valid TCP port but probe reported it free", p)
		}
	}
}

func TestReserveRejectsOutOfRangePort(t *testing.T) {
	s := newTestStore(t)
	for _, p := range []int{-1, 70000} {
		_, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: p})
		if err == nil {
			t.Fatalf("expected out-of-range error for port %d", p)
		}
		if !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("port %d: unhelpful error %v", p, err)
		}
	}
}

// Force skips the in-use check, not the sanity check.
func TestReserveForceStillRejectsOutOfRangePort(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 70000, Force: true}); err == nil {
		t.Fatal("Force must not admit an out-of-range port")
	}
}

// Exercises the real probe rather than a stub: a port with a live listener must
// read as occupied, and the same port must read as free once it is closed.
func TestPortFreeDetectsRealListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if portFree(port) {
		t.Fatalf("port %d has a live listener but probe reported it free", port)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !portFree(port) {
		t.Fatalf("port %d was closed but probe still reports it occupied", port)
	}
}
