package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatePathUsesXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdgtest")
	got, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath() error: %v", err)
	}
	want := "/tmp/xdgtest/portwarden/reservations.json"
	if got != want {
		t.Fatalf("StatePath() = %q, want %q", got, want)
	}
}

func TestStatePathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/hometest")
	got, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath() error: %v", err)
	}
	want := filepath.Join("/tmp/hometest", ".local", "state", "portwarden", "reservations.json")
	if got != want {
		t.Fatalf("StatePath() = %q, want %q", got, want)
	}
}

func TestPortRangeFromEnv(t *testing.T) {
	t.Setenv("PORTWARDEN_PORT_RANGE", "30000-31000")
	low, high := PortRangeFromEnv(20000, 29999)
	if low != 30000 || high != 31000 {
		t.Fatalf("PortRangeFromEnv() = %d-%d, want 30000-31000", low, high)
	}

	// A range reaching past the TCP maximum would allocate ports nothing can
	// bind, so it falls back rather than being honored or clamped silently.
	t.Setenv("PORTWARDEN_PORT_RANGE", "60000-70000")
	low, high = PortRangeFromEnv(20000, 29999)
	if low != 20000 || high != 29999 {
		t.Fatalf("PortRangeFromEnv() above max = %d-%d, want the 20000-29999 fallback", low, high)
	}

	t.Setenv("PORTWARDEN_PORT_RANGE", "garbage")
	low, high = PortRangeFromEnv(20000, 29999)
	if low != 20000 || high != 29999 {
		t.Fatalf("PortRangeFromEnv() fallback = %d-%d, want 20000-29999", low, high)
	}
}

func TestUnresolvedStatePathFailsOperations(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Chdir(t.TempDir())
	s := New(Config{PIDAlive: func(int) bool { return true }, PortFree: func(int) bool { return true }})
	if _, err := s.List(""); err == nil || !strings.Contains(err.Error(), "state path") {
		t.Fatalf("List with no resolvable state path: err = %v, want a state path error", err)
	}
	if _, err := s.Reserve(ReserveRequest{Owner: "a"}); err == nil || !strings.Contains(err.Error(), "state path") {
		t.Fatalf("Reserve with no resolvable state path: err = %v, want a state path error", err)
	}
	if _, err := os.Stat(".lock"); !os.IsNotExist(err) {
		t.Fatalf("a lock file was created in the working directory (stat err = %v)", err)
	}
}
