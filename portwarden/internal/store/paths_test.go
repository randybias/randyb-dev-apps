package store

import (
	"path/filepath"
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

	t.Setenv("PORTWARDEN_PORT_RANGE", "garbage")
	low, high = PortRangeFromEnv(20000, 29999)
	if low != 20000 || high != 29999 {
		t.Fatalf("PortRangeFromEnv() fallback = %d-%d, want 20000-29999", low, high)
	}
}
