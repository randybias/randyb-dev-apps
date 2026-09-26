# randyb-dev-apps

Small convenience applications for agentic software development. One Go
module; each app lives in its own top-level directory.

| App | What it does |
|-----|--------------|
| [portwarden](portwarden/) | MCP server that hands out local TCP ports so concurrent agent sessions don't collide. |

Requirements: Go 1.25+, make, macOS or Linux (uses `flock(2)` and `kill(pid, 0)`).

## portwarden

Agents that each start a dev server on a guessed port collide. portwarden
keeps a shared ledger of port leases at
`$XDG_STATE_HOME/portwarden/reservations.json` (default
`~/.local/state/...`), guarded by `flock`. Every MCP server instance on the
box reads and writes the same file, so sessions in different clients see
each other's reservations.

### Install

    curl -fsSL https://raw.githubusercontent.com/randybias/randyb-dev-apps/main/portwarden/install.sh | bash

Clones to `~/.local/share/randyb-dev-apps`, builds, installs
`~/.local/bin/portwarden`, and runs
`claude mcp add portwarden --scope user`. From a checkout,
`bash portwarden/install.sh` builds that checkout instead. Overrides:
`PORTWARDEN_SRC_DIR`, `PORTWARDEN_BIN_DIR`.

Other MCP clients: run `portwarden` as a stdio server with no arguments.

### Tools

| Tool | Args | Result |
|------|------|--------|
| `reserve_port` | `owner`, `purpose`, `port?`, `ttl_seconds?`, `pid?`, `force?` | The reservation. No `port`: lowest free port in the managed range. |
| `renew_port` | `port`, `ttl_seconds?` | Lease extended to now + TTL. |
| `release_port` | `port` | Reservation removed. |
| `list_reservations` | `owner?` | All reservations, expired ones flagged. |
| `reap` | none | Expired and dead-PID reservations removed and returned. |

### Behavior

- Default lease is 30 days. Stale leases are swept on every mutating call.
- A lease with a `pid` is reclaimed as soon as that process is gone.
- "Free" means unreserved in the ledger and bindable right now: the
  allocator test-binds `0.0.0.0`, `127.0.0.1`, `[::]`, and `[::1]`.
- A requested port that is already bound is rejected. `force` claims it
  anyway (for a lease that expired while its server kept running). `force`
  never overrides another active reservation.
- The bind test releases the port before returning. An unreserved process
  can still take it first, so handle bind failure.

### Configuration

| Env var | Default |
|---------|---------|
| `PORTWARDEN_PORT_RANGE` | `20000-29999` (malformed values fall back to the default) |
| `XDG_STATE_HOME` | `~/.local/state` |

### Agent usage

Reserve before binding, pass the server's PID, renew on restart, and
release on teardown. Never hardcode a port.

## Development

    make            # fmt, vet, test, build -> bin/<app>
    make test
    make build APP=portwarden

## License

Apache 2.0. See [LICENSE](LICENSE).
