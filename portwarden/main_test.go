package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/randyb-dev-apps/portwarden/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(store.Config{
		Path:       filepath.Join(t.TempDir(), "reservations.json"),
		RangeLow:   20000,
		RangeHigh:  20005,
		DefaultTTL: time.Hour,
		Now:        time.Now,
		PIDAlive:   func(int) bool { return true },
	})
}

func TestReserveHandlerReturnsPort(t *testing.T) {
	st := testStore(t)
	h := reserveHandler(st)
	_, out, err := h(context.Background(), &mcp.CallToolRequest{}, reserveIn{Owner: "a", Purpose: "web"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.Port != 20000 {
		t.Fatalf("expected port 20000, got %d", out.Port)
	}
	if out.ExpiresAt == "" {
		t.Fatal("expected expires_at to be set")
	}
}

func TestListHandlerMarksExpired(t *testing.T) {
	// A past-clock store: the reservation's lease ends in the year 2000, which
	// is already expired relative to the handler's real-time check.
	st := store.New(store.Config{
		Path:       filepath.Join(t.TempDir(), "reservations.json"),
		RangeLow:   20000,
		RangeHigh:  20005,
		DefaultTTL: time.Hour,
		Now:        func() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) },
		PIDAlive:   func(int) bool { return true },
	})
	if _, err := st.Reserve(store.ReserveRequest{Owner: "a", Purpose: "x", Port: 20000}); err != nil {
		t.Fatal(err)
	}
	h := listHandler(st)
	_, out, err := h(context.Background(), &mcp.CallToolRequest{}, listIn{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(out.Reservations) != 1 || !out.Reservations[0].Expired {
		t.Fatalf("expected one expired reservation, got %+v", out.Reservations)
	}
}

func TestReserveHandlerConflictIsError(t *testing.T) {
	st := testStore(t)
	h := reserveHandler(st)
	if _, _, err := h(context.Background(), &mcp.CallToolRequest{}, reserveIn{Owner: "a", Purpose: "x", Port: 20000}); err != nil {
		t.Fatal(err)
	}
	_, _, err := h(context.Background(), &mcp.CallToolRequest{}, reserveIn{Owner: "b", Purpose: "y", Port: 20000})
	if err == nil {
		t.Fatal("expected conflict error from handler")
	}
}
