# portwarden

A tiny MCP server that lets concurrent agentic coding sessions reserve local
TCP ports without colliding. Reservations are leases (default 30 days) held in
a shared, lock-protected JSON file; stale leases are reaped lazily on every
mutating call.

## Build

    make build        # -> bin/portwarden

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

## Tools

| Tool | Args | Purpose |
|------|------|---------|
| `reserve_port` | `owner`, `purpose`, `port?`, `ttl_seconds?`, `pid?`, `force?` | Reserve a specific or auto-allocated port. Auto-allocation skips ports a process is already bound to; a requested port that is bound is rejected unless `force` is set. |
| `renew_port` | `port`, `ttl_seconds?` | Extend a lease (heartbeat). |
| `release_port` | `port` | Free a reservation. |
| `list_reservations` | `owner?` | List reservations, marking expired ones. |
| `reap` | — | Reclaim expired / dead-PID reservations now. |

## Configuration

| Env var | Default | Meaning |
|---------|---------|---------|
| `PORTWARDEN_PORT_RANGE` | `20000-29999` | Managed auto-allocation range. |
| `XDG_STATE_HOME` | `~/.local/state` | Base dir for `portwarden/reservations.json`. |

## Notes

- Pass `pid` when you know the server process PID so the lane is reclaimed
  early if that process dies.
- Always `renew_port` when (re)starting a long-lived server to keep the lease
  fresh.
- `force` is for reclaiming a lane whose lease expired while its process kept
  running. It never overrides another session's active reservation.
- The bind probe is a snapshot, not a lock: an unreserved process can still
  take the port before your server binds it, so handle bind failure.
