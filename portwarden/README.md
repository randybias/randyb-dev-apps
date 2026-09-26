# portwarden

MCP server that lets concurrent agentic coding sessions reserve local TCP
ports without colliding. Reservations are leases (default 30 days) held in a
shared, `flock`-protected JSON file that every portwarden instance on the box
reads and writes, so sessions in different MCP clients see each other's
reservations. Stale leases are swept on every mutating call.

## Requirements

Go 1.25+, make, macOS or Linux (uses `flock(2)` and `kill(pid, 0)`).

## Build

From the repo root:

    make build APP=portwarden   # -> bin/portwarden
    make test APP=portwarden
    make APP=portwarden         # fmt, vet, test, build

## Install (any dev box)

From a local checkout:

    bash portwarden/install.sh

Remote bootstrap:

    curl -fsSL https://raw.githubusercontent.com/randybias/randyb-dev-apps/main/portwarden/install.sh | bash

The installer clones/updates the repo to `~/.local/share/randyb-dev-apps`,
builds, installs `portwarden` to `~/.local/bin`, and registers it as a
user-scope MCP server. Override with `PORTWARDEN_SRC_DIR` / `PORTWARDEN_BIN_DIR`.

## Register as an MCP server (Claude Code)

    claude mcp add portwarden --scope user -- /absolute/path/to/randyb-dev-apps/bin/portwarden

Or add to your MCP config:

    {
      "mcpServers": {
        "portwarden": {
          "command": "/absolute/path/to/randyb-dev-apps/bin/portwarden"
        }
      }
    }

Other MCP clients: run `portwarden` as a stdio server with no arguments.

## Tools

| Tool | Args | Purpose |
|------|------|---------|
| `reserve_port` | `owner`, `purpose`, `port?`, `ttl_seconds?`, `pid?`, `force?` | Reserve a specific or auto-allocated port. Auto-allocation skips ports a process is already bound to; a requested port that is bound is rejected unless `force` is set. |
| `renew_port` | `port`, `ttl_seconds?` | Extend a lease (heartbeat). |
| `release_port` | `port` | Free a reservation. |
| `list_reservations` | `owner?` | List reservations, marking expired ones. |
| `reap` | — | Reclaim expired / dead-PID reservations now; returns what was removed. |

## Configuration

| Env var | Default | Meaning |
|---------|---------|---------|
| `PORTWARDEN_PORT_RANGE` | `20000-29999` | Managed auto-allocation range, `low-high`. Malformed values fall back to the default. |
| `XDG_STATE_HOME` | `~/.local/state` | Base dir for `portwarden/reservations.json`. |

## Notes

- Reserve before binding and never hardcode a port. Release on teardown.
- A port is free when it is unreserved in the ledger and bindable right now:
  the allocator test-binds `0.0.0.0`, `127.0.0.1`, `[::]`, and `[::1]`.
- Pass `pid` when you know the server process PID so the lane is reclaimed
  early if that process dies.
- Always `renew_port` when (re)starting a long-lived server to keep the lease
  fresh.
- `force` is for reclaiming a lane whose lease expired while its process kept
  running. It never overrides another session's active reservation.
- The bind probe is a snapshot, not a lock: an unreserved process can still
  take the port before your server binds it, so handle bind failure.
