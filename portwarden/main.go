// Command portwarden is an MCP server that arbitrates local dev port
// reservations across concurrent agentic coding sessions.
package main

import (
	"context"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/randybias/randyb-dev-apps/portwarden/internal/store"
)

const version = "0.1.0"

// --- tool I/O types ---

type reserveIn struct {
	Owner      string `json:"owner" jsonschema:"who owns this reservation (project or session name)"`
	Purpose    string `json:"purpose" jsonschema:"what the port is for"`
	Port       int    `json:"port,omitempty" jsonschema:"specific port to request; omit to auto-allocate from the managed range"`
	TTLSeconds int    `json:"ttl_seconds,omitempty" jsonschema:"lease duration in seconds; omit for the default (30 days)"`
	PID        int    `json:"pid,omitempty" jsonschema:"PID of the process using the port; lets the lane be reclaimed early if that process dies"`
	Force      bool   `json:"force,omitempty" jsonschema:"claim a specific port even though a process is already bound to it; for reclaiming a lane whose lease expired while the process kept running. Never overrides another session's active reservation"`
}

type reservationOut struct {
	Port      int    `json:"port"`
	Owner     string `json:"owner"`
	Purpose   string `json:"purpose"`
	PID       int    `json:"pid,omitempty"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	Expired   bool   `json:"expired,omitempty"`
}

type renewIn struct {
	Port       int `json:"port" jsonschema:"the reserved port to renew"`
	TTLSeconds int `json:"ttl_seconds,omitempty" jsonschema:"new lease duration in seconds; omit for the default (30 days)"`
}

type releaseIn struct {
	Port int `json:"port" jsonschema:"the reserved port to release"`
}

type okOut struct {
	OK bool `json:"ok"`
}

type listIn struct {
	Owner string `json:"owner,omitempty" jsonschema:"optional owner filter"`
}

type listOut struct {
	Reservations []reservationOut `json:"reservations"`
}

type reapIn struct{}

type reapOut struct {
	Reclaimed []reservationOut `json:"reclaimed"`
}

// toOut converts a store.Reservation to wire form. A non-zero ref marks the
// Expired flag (used by list); pass the zero time to skip the check.
func toOut(r store.Reservation, ref time.Time) reservationOut {
	out := reservationOut{
		Port:      r.Port,
		Owner:     r.Owner,
		Purpose:   r.Purpose,
		PID:       r.PID,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: r.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if !ref.IsZero() && ref.After(r.ExpiresAt) {
		out.Expired = true
	}
	return out
}

func ttl(seconds int) time.Duration { return time.Duration(seconds) * time.Second }

// --- handlers (constructors return SDK-shaped handler funcs) ---

func reserveHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, reserveIn) (*mcp.CallToolResult, reservationOut, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in reserveIn) (*mcp.CallToolResult, reservationOut, error) {
		r, err := st.Reserve(store.ReserveRequest{
			Owner: in.Owner, Purpose: in.Purpose, Port: in.Port, TTL: ttl(in.TTLSeconds), PID: in.PID,
			Force: in.Force,
		})
		if err != nil {
			return nil, reservationOut{}, err
		}
		return nil, toOut(r, time.Time{}), nil
	}
}

func renewHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, renewIn) (*mcp.CallToolResult, reservationOut, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in renewIn) (*mcp.CallToolResult, reservationOut, error) {
		r, err := st.Renew(in.Port, ttl(in.TTLSeconds))
		if err != nil {
			return nil, reservationOut{}, err
		}
		return nil, toOut(r, time.Time{}), nil
	}
}

func releaseHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, releaseIn) (*mcp.CallToolResult, okOut, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in releaseIn) (*mcp.CallToolResult, okOut, error) {
		if err := st.Release(in.Port); err != nil {
			return nil, okOut{}, err
		}
		return nil, okOut{OK: true}, nil
	}
}

func listHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, listIn) (*mcp.CallToolResult, listOut, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
		rs, err := st.List(in.Owner)
		if err != nil {
			return nil, listOut{}, err
		}
		now := time.Now().UTC()
		out := listOut{Reservations: make([]reservationOut, 0, len(rs))}
		for _, r := range rs {
			out.Reservations = append(out.Reservations, toOut(r, now))
		}
		return nil, out, nil
	}
}

func reapHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, reapIn) (*mcp.CallToolResult, reapOut, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ reapIn) (*mcp.CallToolResult, reapOut, error) {
		rs, err := st.Reap()
		if err != nil {
			return nil, reapOut{}, err
		}
		out := reapOut{Reclaimed: make([]reservationOut, 0, len(rs))}
		for _, r := range rs {
			out.Reclaimed = append(out.Reclaimed, toOut(r, time.Time{}))
		}
		return nil, out, nil
	}
}

func main() {
	st := store.New(store.Config{})
	server := mcp.NewServer(&mcp.Implementation{Name: "portwarden", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{Name: "reserve_port", Description: "Reserve a local TCP port (specific or auto-allocated) with a lease. Ports already bound by a process are skipped when auto-allocating, and rejected when requested by number unless force is set. A non-forced reservation was bindable when probed, but the probe releases the port before returning, so callers must still handle bind failure."}, reserveHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "renew_port", Description: "Extend the lease on a reserved port (heartbeat)."}, renewHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "release_port", Description: "Release a reserved port."}, releaseHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "list_reservations", Description: "List current port reservations, marking expired ones."}, listHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "reap", Description: "Reclaim expired and dead-process reservations now."}, reapHandler(st))

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("portwarden: %v", err)
	}
}
