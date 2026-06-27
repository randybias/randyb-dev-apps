# portwarden — local dev port reservation MCP server

**Date:** 2026-06-27
**Status:** Design approved, pending implementation plan
**Repo:** `randyb-dev-apps` (first of several local dev utility apps)

## Purpose

Let independent agentic coding sessions (multiple concurrent Claude Code
sessions, etc.) reserve local TCP ports for development software — local web
servers, database servers, and the like — so they don't collide. The server
maintains shared reservation state across sessions, arbitrates lanes, and reaps
stale reservations over time.

Constraints that shaped the design:

- Very small token surface; invokable by any agent; on by default.
- Maximally portable across Linux and macOS.
- Local software development only (no remote/network use).
- Must arbitrate across many concurrent sessions.
- Track time (leases) so old reservations can be reaped.

## Architecture

**Stateless stdio MCP server in Go**, one process per session. All shared state
lives in a single file on disk; every mutating call performs a lock-protected
read-modify-write. There is **no daemon and no background process** — nothing to
start, supervise, or recover. "On by default" means the compiled binary is
registered as an MCP server in the agent's config.

This is the "shared state file + file locking" model (chosen over a central
daemon): for local dev with a handful of sessions, file locking is sufficient,
maximally portable, and has zero lifecycle to manage.

## MCP protocol

Uses the **official MCP Go SDK** (`github.com/modelcontextprotocol/go-sdk`).
The SDK handles JSON-RPC framing, `initialize` / `tools/list` / `tools/call`,
and typed tool input/output schemas via Go structs. Standardizing on this SDK is
intended for *all* future MCP apps in this repo, not just portwarden.

Transport: stdio.

## State store

- Single JSON file at `$XDG_STATE_HOME/portwarden/reservations.json`, falling
  back to `~/.local/state/portwarden/reservations.json` when `XDG_STATE_HOME`
  is unset. This path is portable across Linux and macOS.
- JSON (not SQLite): human-readable, debuggable, no cgo dependency, and our
  concurrency level is low enough that it is unnecessary.
- **Concurrency:** an exclusive advisory lock (`syscall.Flock`, available on
  both Linux and macOS) on a sidecar `reservations.json.lock` wraps the entire
  read-modify-write cycle. The lock is released when the call completes.
- **Crash safety:** writes are atomic — write to a temp file in the same
  directory, then `rename` over the target. A crash mid-write never corrupts the
  live state file.
- **Corruption handling:** an unreadable or malformed state file surfaces a
  loud error. It is never silently truncated or wiped.

## Reservation record

```json
{
  "port": 20001,
  "owner": "myproject",
  "purpose": "local postgres for integration tests",
  "pid": 48213,
  "created_at": "2026-06-27T13:00:00Z",
  "expires_at": "2026-07-27T13:00:00Z"
}
```

- `owner` and `purpose` are caller-provided (the agent's responsibility).
- `pid` is optional (see lease model).
- Timestamps are RFC 3339 / ISO 8601 UTC.

## Lease model

Lease-based reservations with **lazy expiry** and a **renewal heartbeat** — the
same proven pattern as DHCP leases and distributed-lock TTLs. It needs no
background process, which fits the no-daemon architecture.

- **`reserve_port`** sets `expires_at = now + ttl`. Default TTL **30 days**,
  overridable per call. Allocation follows model 3:
  - If a specific `port` is requested and free, grant it.
  - Otherwise allocate the lowest free port from the managed range.
  - **Managed range default: 20000–29999** (configurable via env var). Chosen
    to sit below both the Linux (32768–60999) and macOS (49152–65535) OS
    ephemeral port ranges, so portwarden allocations never fight the kernel's
    own ephemeral allocator.
- **`renew_port`** pushes `expires_at = now + ttl` forward — an idempotent
  heartbeat. Agents call this when (re)starting a server.
- **Lazy reaping:** every *mutating* call (`reserve_port`, `renew_port`,
  `release_port`, `reap`) first sweeps and removes:
  1. reservations past their `expires_at`, and
  2. reservations whose provided `pid` is provably dead (a signal-0 /
     `kill -0`-style existence check — cheap and reliable; this is process
     existence, not port-binding liveness).

  ...then performs its own work. The next session to touch the store cleans up
  the dead lanes. Reads do not mutate.
- **Liveness/port-binding is explicitly NOT tracked.** A reservation can be
  valid while nothing is currently listening (server stopped overnight, not yet
  started). That is not a reason to reap. Reaping is conservative: lease expiry
  is the backstop; optional PID death enables faster, discipline-free
  reclamation when the agent supplies a PID.

## Tool surface (5 tools)

| Tool | Args | Behavior |
|------|------|----------|
| `reserve_port` | `owner`, `purpose`, `port?`, `ttl_seconds?`, `pid?` | Reserve a specific port if given/free, else allocate from the range. Returns the port, `expires_at`, and the full record. Errors if a requested port is already reserved. |
| `renew_port` | `port`, `ttl_seconds?` | Push the lease's `expires_at` forward. Errors if no active reservation for that port. |
| `release_port` | `port` | Explicitly free a reservation. |
| `list_reservations` | `owner?` | Report all reservations (optionally filtered by owner), each annotated active/expired. Pure read — no mutation. |
| `reap` | — | Run the sweep on demand (same logic as the lazy sweep). Returns what was reclaimed. |

Errors are returned as structured tool errors: a port-taken error names the
conflicting `owner`; invalid port/range and malformed args are reported clearly.

## Repo layout (designed for many apps)

```
randyb-dev-apps/
  go.mod                       # module github.com/randybias/randyb-dev-apps
  go.sum
  portwarden/
    main.go                    # wires the MCP SDK server + tool handlers
    internal/store/            # reservation logic: allocator, sweep, lock,
                               #   atomic persistence — unit-testable, no
                               #   coupling to the MCP/protocol layer
    README.md                  # usage + MCP registration snippet
  bin/                         # built binaries (gitignored)
  docs/superpowers/specs/
  .gitignore
```

Single Go module at the repo root; each app is its own subdirectory with its own
`main` package and binary. Build: `go build -o bin/portwarden ./portwarden`.
Module path uses `github.com/randybias` per project convention.

## Testing (TDD)

- Core `internal/store` logic is unit-tested in isolation from the protocol
  layer: allocation (specific + range fallback + exhaustion), TTL expiry sweep,
  PID-death reap, lock-protected concurrent read-modify-write, atomic-write
  durability, and corruption handling.
- One integration test drives the assembled MCP server through the tool calls
  end to end.
- Test runs are scoped and bounded (single package/pattern, no unbounded
  watchers or auto worker pools).

## Out of scope (v1)

- Port-binding / liveness checks.
- The companion `randyb-general-dev` skill (a separate follow-up, authored via
  the skill-creator skill, that instructs agents to renew their lease whenever
  they start a server). Standardizing the broader `~/code` randyb-skills set is
  also separate.
- Any central daemon or always-on process.

## Open trivia (defaulted, easily changed)

- Name: **portwarden** (chosen to avoid the existing `@apideck/portman` OSS tool).
- Managed port range default: **20000–29999**.
