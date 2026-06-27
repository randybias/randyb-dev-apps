# portwarden

A tiny MCP server that lets concurrent agentic coding sessions reserve local
TCP ports without colliding. Reservations are leases (default 30 days) held in
a shared, lock-protected JSON file; stale leases are reaped lazily on every
mutating call.

## Build

    make build        # -> bin/portwarden

## Register as an MCP server (Claude Code)

    claude mcp add portwarden -- /absolute/path/to/randyb-dev-apps/bin/portwarden

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
| `reserve_port` | `owner`, `purpose`, `port?`, `ttl_seconds?`, `pid?` | Reserve a specific or auto-allocated port. |
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
