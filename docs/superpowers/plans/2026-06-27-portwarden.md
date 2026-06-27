# portwarden Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `portwarden`, a stdio MCP server that lets concurrent agentic coding sessions reserve local TCP ports without collision, backed by a shared lock-protected JSON state file with lease-based expiry.

**Architecture:** Stateless Go binary, one process per session. All shared state lives in a single JSON file; every mutating tool call performs a `flock`-protected read-modify-write that first lazily reaps expired/dead-PID reservations. No daemon. The MCP protocol layer uses the official MCP Go SDK; all reservation logic lives in an SDK-independent `internal/store` package that is unit-tested in isolation.

**Tech Stack:** Go 1.23+, official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`), stdlib `syscall` for `Flock`/`Kill`. JSON state file. Make for build/test.

## Global Constraints

- Module path: `github.com/randybias/randyb-dev-apps` (per project convention — `randybias`, not `rbias`).
- Go floor: `go 1.23` in `go.mod` (SDK minimum; maximizes cross-box portability).
- Only external dependency permitted: the official MCP Go SDK. Everything else stdlib.
- Platforms: Linux and macOS only. `syscall.Flock`/`syscall.Kill` are unix-only; Windows is explicitly out of scope (no build tags needed in v1).
- Managed port range default: **20000–29999**, overridable via env `PORTWARDEN_PORT_RANGE` (format `low-high`).
- Default lease TTL: **30 days** (`30 * 24h`).
- State file: `$XDG_STATE_HOME/portwarden/reservations.json`, falling back to `~/.local/state/portwarden/reservations.json`.
- All timestamps are RFC 3339 UTC.
- Commits: Conventional Commits, no emojis. Commit at the end of each task.
- Tests: bounded runs only — scope to `./portwarden/...` with `-count=1`. Never an unbounded repo-wide watcher.
- Repo is structured for many apps: one Go module at root, each app in its own subdirectory with its own `main`.

---

### Task 1: Project scaffold, Makefile, and state-path resolution

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `Makefile`
- Create: `portwarden/internal/store/paths.go`
- Test: `portwarden/internal/store/paths_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `store.StatePath() (string, error)` — resolves the reservations.json path.
  - `store.PortRangeFromEnv(defLow, defHigh int) (low, high int)` — parses `PORTWARDEN_PORT_RANGE`, falling back to defaults on unset/malformed input.

- [ ] **Step 1: Initialize the module and Makefile**

```bash
cd /Users/rbias/code/randyb-dev-apps
go mod init github.com/randybias/randyb-dev-apps
```

Then edit `go.mod` so the go directive reads exactly:

```
go 1.23
```

Create `Makefile` (note: recipes must use TAB indentation):

```makefile
APP ?= portwarden
BIN_DIR := bin

.PHONY: all build test fmt vet tidy clean

all: fmt vet test build

build:
	go build -o $(BIN_DIR)/$(APP) ./$(APP)

test:
	go test ./$(APP)/... -count=1

fmt:
	gofmt -w ./$(APP)

vet:
	go vet ./$(APP)/...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
```

- [ ] **Step 2: Write the failing test**

Create `portwarden/internal/store/paths_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./portwarden/internal/store/ -run TestStatePath -count=1`
Expected: build failure / FAIL — `undefined: StatePath`.

- [ ] **Step 4: Write minimal implementation**

Create `portwarden/internal/store/paths.go`:

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StatePath resolves the path to the shared reservations file, honoring
// XDG_STATE_HOME and falling back to ~/.local/state.
func StatePath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "portwarden", "reservations.json"), nil
}

// PortRangeFromEnv parses PORTWARDEN_PORT_RANGE ("low-high"), returning the
// provided defaults when the variable is unset or malformed.
func PortRangeFromEnv(defLow, defHigh int) (int, int) {
	raw := os.Getenv("PORTWARDEN_PORT_RANGE")
	if raw == "" {
		return defLow, defHigh
	}
	parts := strings.SplitN(raw, "-", 2)
	if len(parts) != 2 {
		return defLow, defHigh
	}
	low, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	high, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || low <= 0 || high < low {
		return defLow, defHigh
	}
	return low, high
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./portwarden/internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod Makefile portwarden/internal/store/paths.go portwarden/internal/store/paths_test.go
git commit -m "feat(portwarden): scaffold module, makefile, and state-path resolution"
```

---

### Task 2: Reservation model, state load/save, and locking

**Files:**
- Create: `portwarden/internal/store/store.go`
- Test: `portwarden/internal/store/store_test.go`

**Interfaces:**
- Consumes: `StatePath`, `PortRangeFromEnv` (Task 1).
- Produces:
  - `type Reservation struct { Port int; Owner, Purpose string; PID int; CreatedAt, ExpiresAt time.Time }`
  - `type Config struct { Path string; RangeLow, RangeHigh int; DefaultTTL time.Duration; Now func() time.Time; PIDAlive func(int) bool }`
  - `type Store struct { ... }` with `func New(cfg Config) *Store`.
  - Internal: `loadState`, `saveState`, `(*Store).withLock`, `(*Store).now`. (`withLock` and the on-disk `state` struct are consumed by Tasks 3–5.)

- [ ] **Step 1: Write the failing test**

Create `portwarden/internal/store/store_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return New(Config{
		Path:       filepath.Join(dir, "reservations.json"),
		RangeLow:   20000,
		RangeHigh:  20003, // tiny range so exhaustion is testable
		DefaultTTL: time.Hour,
		Now:        func() time.Time { return time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC) },
		PIDAlive:   func(int) bool { return true },
	})
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	st := &state{Reservations: []Reservation{
		{Port: 20000, Owner: "a", Purpose: "x", CreatedAt: s.now(), ExpiresAt: s.now().Add(time.Hour)},
	}}
	if err := saveState(s.cfg.Path, st); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got, err := loadState(s.cfg.Path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(got.Reservations) != 1 || got.Reservations[0].Port != 20000 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s := newTestStore(t)
	got, err := loadState(s.cfg.Path)
	if err != nil {
		t.Fatalf("loadState on missing file should not error: %v", err)
	}
	if len(got.Reservations) != 0 {
		t.Fatalf("expected empty state, got %+v", got)
	}
}

func TestLoadCorruptFileErrors(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.cfg.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.cfg.Path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(s.cfg.Path); err == nil {
		t.Fatal("expected error on corrupt state file, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./portwarden/internal/store/ -run 'TestSaveLoad|TestLoad' -count=1`
Expected: build failure — `undefined: state`, `undefined: saveState`, etc.

- [ ] **Step 3: Write minimal implementation**

Create `portwarden/internal/store/store.go`:

```go
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Reservation is one held port lane.
type Reservation struct {
	Port      int       `json:"port"`
	Owner     string    `json:"owner"`
	Purpose   string    `json:"purpose"`
	PID       int       `json:"pid,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// state is the on-disk document.
type state struct {
	Reservations []Reservation `json:"reservations"`
}

// Config configures a Store. Zero-value fields are filled with defaults by New.
type Config struct {
	Path       string
	RangeLow   int
	RangeHigh  int
	DefaultTTL time.Duration
	Now        func() time.Time
	PIDAlive   func(int) bool
}

// Store performs lock-protected reservation operations against the state file.
type Store struct {
	cfg Config
}

const (
	defaultRangeLow  = 20000
	defaultRangeHigh = 29999
	defaultTTL       = 30 * 24 * time.Hour
)

// New returns a Store, filling unset Config fields with defaults (including
// env-driven port range and the resolved state path).
func New(cfg Config) *Store {
	if cfg.Path == "" {
		if p, err := StatePath(); err == nil {
			cfg.Path = p
		}
	}
	if cfg.RangeLow == 0 && cfg.RangeHigh == 0 {
		cfg.RangeLow, cfg.RangeHigh = PortRangeFromEnv(defaultRangeLow, defaultRangeHigh)
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = defaultTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PIDAlive == nil {
		cfg.PIDAlive = pidAlive
	}
	return &Store{cfg: cfg}
}

func (s *Store) now() time.Time { return s.cfg.Now().UTC() }

// pidAlive reports whether a process exists. EPERM means it exists but is owned
// by another user; ESRCH means it is gone.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func loadState(path string) (*state, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &state{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if len(data) == 0 {
		return &state{}, nil
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state %s (refusing to overwrite): %w", path, err)
	}
	return &st, nil
}

// saveState writes atomically: temp file in the same dir, then rename.
func saveState(path string, st *state) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "reservations-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// withLock acquires an advisory lock around a load -> fn -> (conditional) save
// cycle. exclusive=false takes a shared lock and never saves.
func (s *Store) withLock(exclusive bool, fn func(st *state) (changed bool, err error)) error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.Path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	lockPath := s.cfg.Path + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()

	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(lf.Fd()), how); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	st, err := loadState(s.cfg.Path)
	if err != nil {
		return err
	}
	changed, err := fn(st)
	if err != nil {
		return err
	}
	if changed {
		return saveState(s.cfg.Path, st)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./portwarden/internal/store/ -count=1`
Expected: PASS (Task 1 tests still pass too).

- [ ] **Step 5: Commit**

```bash
git add portwarden/internal/store/store.go portwarden/internal/store/store_test.go
git commit -m "feat(portwarden): add reservation model, atomic state I/O, and flock"
```

---

### Task 3: Lazy reap sweep (expiry + dead PID)

**Files:**
- Modify: `portwarden/internal/store/store.go` (add `sweep`)
- Test: `portwarden/internal/store/sweep_test.go`

**Interfaces:**
- Consumes: `state`, `Reservation`, `Config.Now`, `Config.PIDAlive`.
- Produces: `func sweep(st *state, now time.Time, alive func(int) bool) []Reservation` — removes expired and dead-PID reservations in place, returns the removed ones.

- [ ] **Step 1: Write the failing test**

Create `portwarden/internal/store/sweep_test.go`:

```go
package store

import (
	"testing"
	"time"
)

func TestSweepRemovesExpiredAndDeadPID(t *testing.T) {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	st := &state{Reservations: []Reservation{
		{Port: 1, Owner: "live", ExpiresAt: now.Add(time.Hour)},                 // keep: active
		{Port: 2, Owner: "expired", ExpiresAt: now.Add(-time.Minute)},           // drop: expired
		{Port: 3, Owner: "deadpid", PID: 999, ExpiresAt: now.Add(time.Hour)},    // drop: dead pid
		{Port: 4, Owner: "livepid", PID: 1000, ExpiresAt: now.Add(time.Hour)},   // keep: live pid
	}}
	alive := func(pid int) bool { return pid == 1000 }

	removed := sweep(st, now, alive)

	if len(st.Reservations) != 2 {
		t.Fatalf("expected 2 kept, got %d: %+v", len(st.Reservations), st.Reservations)
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
	for _, r := range st.Reservations {
		if r.Port != 1 && r.Port != 4 {
			t.Fatalf("unexpected survivor: port %d", r.Port)
		}
	}
}

func TestSweepKeepsZeroPIDWithinLease(t *testing.T) {
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	st := &state{Reservations: []Reservation{
		{Port: 5, Owner: "nopid", PID: 0, ExpiresAt: now.Add(time.Hour)},
	}}
	removed := sweep(st, now, func(int) bool { return false })
	if len(removed) != 0 || len(st.Reservations) != 1 {
		t.Fatalf("PID 0 within lease must be kept; removed=%d kept=%d", len(removed), len(st.Reservations))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./portwarden/internal/store/ -run TestSweep -count=1`
Expected: build failure — `undefined: sweep`.

- [ ] **Step 3: Write minimal implementation**

Append to `portwarden/internal/store/store.go`:

```go
// sweep removes expired and provably-dead-PID reservations in place and
// returns those it removed. A reservation with PID 0 is judged on lease only.
func sweep(st *state, now time.Time, alive func(int) bool) []Reservation {
	kept := make([]Reservation, 0, len(st.Reservations))
	var removed []Reservation
	for _, r := range st.Reservations {
		expired := now.After(r.ExpiresAt)
		dead := r.PID != 0 && !alive(r.PID)
		if expired || dead {
			removed = append(removed, r)
			continue
		}
		kept = append(kept, r)
	}
	st.Reservations = kept
	return removed
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./portwarden/internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portwarden/internal/store/store.go portwarden/internal/store/sweep_test.go
git commit -m "feat(portwarden): add lazy reap sweep for expired and dead-pid leases"
```

---

### Task 4: Reserve (specific + range allocation, exhaustion, conflict)

**Files:**
- Modify: `portwarden/internal/store/store.go` (add `ReserveRequest`, `Reserve`)
- Test: `portwarden/internal/store/reserve_test.go`

**Interfaces:**
- Consumes: `withLock`, `sweep`, `Config`.
- Produces:
  - `type ReserveRequest struct { Owner, Purpose string; Port int; TTL time.Duration; PID int }`
  - `func (s *Store) Reserve(req ReserveRequest) (Reservation, error)` — Port 0 auto-allocates the lowest free port in range; a specific Port errors if taken; range exhaustion errors. Sweeps before allocating.

- [ ] **Step 1: Write the failing test**

Create `portwarden/internal/store/reserve_test.go`:

```go
package store

import (
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./portwarden/internal/store/ -run TestReserve -count=1`
Expected: build failure — `undefined: ReserveRequest` / `Reserve`.

- [ ] **Step 3: Write minimal implementation**

Append to `portwarden/internal/store/store.go`:

```go
// ReserveRequest describes a reservation. Port 0 means auto-allocate; TTL 0
// means use the configured default.
type ReserveRequest struct {
	Owner   string
	Purpose string
	Port    int
	TTL     time.Duration
	PID     int
}

// Reserve allocates a port lane. It sweeps stale leases first, then honors a
// requested port (error if taken) or allocates the lowest free port in range.
func (s *Store) Reserve(req ReserveRequest) (Reservation, error) {
	if req.Owner == "" {
		return Reservation{}, errors.New("owner is required")
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = s.cfg.DefaultTTL
	}
	now := s.now()
	var result Reservation
	err := s.withLock(true, func(st *state) (bool, error) {
		sweep(st, now, s.cfg.PIDAlive)
		used := make(map[int]Reservation, len(st.Reservations))
		for _, r := range st.Reservations {
			used[r.Port] = r
		}
		port := req.Port
		if port != 0 {
			if existing, taken := used[port]; taken {
				return false, fmt.Errorf("port %d already reserved by %q", port, existing.Owner)
			}
		} else {
			for p := s.cfg.RangeLow; p <= s.cfg.RangeHigh; p++ {
				if _, taken := used[p]; !taken {
					port = p
					break
				}
			}
			if port == 0 {
				return false, fmt.Errorf("no free ports in range %d-%d", s.cfg.RangeLow, s.cfg.RangeHigh)
			}
		}
		result = Reservation{
			Port:      port,
			Owner:     req.Owner,
			Purpose:   req.Purpose,
			PID:       req.PID,
			CreatedAt: now,
			ExpiresAt: now.Add(ttl),
		}
		st.Reservations = append(st.Reservations, result)
		return true, nil
	})
	if err != nil {
		return Reservation{}, err
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./portwarden/internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portwarden/internal/store/store.go portwarden/internal/store/reserve_test.go
git commit -m "feat(portwarden): implement port reservation with range allocation"
```

---

### Task 5: Renew, Release, List, Reap

**Files:**
- Modify: `portwarden/internal/store/store.go`
- Test: `portwarden/internal/store/ops_test.go`

**Interfaces:**
- Consumes: `withLock`, `sweep`, `Reservation`.
- Produces:
  - `func (s *Store) Renew(port int, ttl time.Duration) (Reservation, error)`
  - `func (s *Store) Release(port int) error`
  - `func (s *Store) List(owner string) ([]Reservation, error)` — pure read (shared lock, no sweep, no save).
  - `func (s *Store) Reap() ([]Reservation, error)` — runs the sweep on demand, returns reclaimed.

- [ ] **Step 1: Write the failing test**

Create `portwarden/internal/store/ops_test.go`:

```go
package store

import (
	"testing"
	"time"
)

func TestRenewExtendsLease(t *testing.T) {
	s := newTestStore(t)
	r, _ := s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 20000, TTL: time.Minute})
	want := s.now().Add(2 * time.Hour)
	got, err := s.Renew(20000, 2*time.Hour)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !got.ExpiresAt.Equal(want) {
		t.Fatalf("expiry = %v, want %v", got.ExpiresAt, want)
	}
	_ = r
}

func TestRenewUnknownPortErrors(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Renew(20000, time.Hour); err == nil {
		t.Fatal("expected error renewing unknown port")
	}
}

func TestReleaseRemoves(t *testing.T) {
	s := newTestStore(t)
	s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 20000})
	if err := s.Release(20000); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got, _ := s.List("")
	if len(got) != 0 {
		t.Fatalf("expected empty after release, got %+v", got)
	}
}

func TestReleaseUnknownPortErrors(t *testing.T) {
	s := newTestStore(t)
	if err := s.Release(20000); err == nil {
		t.Fatal("expected error releasing unknown port")
	}
}

func TestListFiltersByOwner(t *testing.T) {
	s := newTestStore(t)
	s.Reserve(ReserveRequest{Owner: "a", Purpose: "x", Port: 20000})
	s.Reserve(ReserveRequest{Owner: "b", Purpose: "y", Port: 20001})
	got, err := s.List("a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Owner != "a" {
		t.Fatalf("expected only owner a, got %+v", got)
	}
}

func TestReap(t *testing.T) {
	s := newTestStore(t)
	// expired reservation seeded directly via Reserve with negative TTL
	s.Reserve(ReserveRequest{Owner: "old", Purpose: "x", Port: 20000, TTL: -time.Minute})
	s.Reserve(ReserveRequest{Owner: "new", Purpose: "y", Port: 20001, TTL: time.Hour})
	reclaimed, err := s.Reap()
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].Owner != "old" {
		t.Fatalf("expected to reclaim 'old', got %+v", reclaimed)
	}
	got, _ := s.List("")
	if len(got) != 1 || got[0].Owner != "new" {
		t.Fatalf("expected only 'new' to remain, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./portwarden/internal/store/ -run 'TestRenew|TestRelease|TestList|TestReap' -count=1`
Expected: build failure — `undefined: ...Renew/Release/List/Reap`.

- [ ] **Step 3: Write minimal implementation**

Append to `portwarden/internal/store/store.go`:

```go
// Renew pushes a reservation's expiry to now+ttl. Sweeps first.
func (s *Store) Renew(port int, ttl time.Duration) (Reservation, error) {
	if ttl <= 0 {
		ttl = s.cfg.DefaultTTL
	}
	now := s.now()
	var result Reservation
	err := s.withLock(true, func(st *state) (bool, error) {
		sweep(st, now, s.cfg.PIDAlive)
		for i := range st.Reservations {
			if st.Reservations[i].Port == port {
				st.Reservations[i].ExpiresAt = now.Add(ttl)
				result = st.Reservations[i]
				return true, nil
			}
		}
		return false, fmt.Errorf("no active reservation for port %d", port)
	})
	if err != nil {
		return Reservation{}, err
	}
	return result, nil
}

// Release frees a reservation. Sweeps first.
func (s *Store) Release(port int) error {
	now := s.now()
	return s.withLock(true, func(st *state) (bool, error) {
		sweep(st, now, s.cfg.PIDAlive)
		out := make([]Reservation, 0, len(st.Reservations))
		found := false
		for _, r := range st.Reservations {
			if r.Port == port {
				found = true
				continue
			}
			out = append(out, r)
		}
		if !found {
			return false, fmt.Errorf("no reservation for port %d", port)
		}
		st.Reservations = out
		return true, nil
	})
}

// List returns reservations (optionally filtered by owner). Pure read.
func (s *Store) List(owner string) ([]Reservation, error) {
	var out []Reservation
	err := s.withLock(false, func(st *state) (bool, error) {
		for _, r := range st.Reservations {
			if owner == "" || r.Owner == owner {
				out = append(out, r)
			}
		}
		return false, nil
	})
	return out, err
}

// Reap runs the stale-lease sweep on demand and returns what it reclaimed.
func (s *Store) Reap() ([]Reservation, error) {
	now := s.now()
	var removed []Reservation
	err := s.withLock(true, func(st *state) (bool, error) {
		removed = sweep(st, now, s.cfg.PIDAlive)
		return len(removed) > 0, nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./portwarden/internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add portwarden/internal/store/store.go portwarden/internal/store/ops_test.go
git commit -m "feat(portwarden): add renew, release, list, and reap operations"
```

---

### Task 6: MCP server wiring, tool handlers, README

**Files:**
- Create: `portwarden/main.go`
- Test: `portwarden/main_test.go`
- Create: `portwarden/README.md`

**Interfaces:**
- Consumes: all of `internal/store`; the official MCP Go SDK.
- Produces: a runnable `portwarden` binary exposing 5 tools over stdio. Handler constructors (`reserveHandler`, `renewHandler`, `releaseHandler`, `listHandler`, `reapHandler`) are package-private and unit-tested directly.

- [ ] **Step 1: Add the SDK dependency**

```bash
cd /Users/rbias/code/randyb-dev-apps
go get github.com/modelcontextprotocol/go-sdk/mcp@latest
go mod tidy
```

- [ ] **Step 2: Write the failing test**

Create `portwarden/main_test.go`:

```go
package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/randybias/randyb-dev-apps/portwarden/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	st := testStore(t)
	if _, err := st.Reserve(store.ReserveRequest{Owner: "a", Purpose: "x", Port: 20000, TTL: -time.Minute}); err != nil {
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./portwarden/ -count=1`
Expected: build failure — `undefined: reserveHandler`, `reserveIn`, etc.

- [ ] **Step 4: Write minimal implementation**

Create `portwarden/main.go`:

```go
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

	mcp.AddTool(server, &mcp.Tool{Name: "reserve_port", Description: "Reserve a local TCP port (specific or auto-allocated) with a lease."}, reserveHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "renew_port", Description: "Extend the lease on a reserved port (heartbeat)."}, renewHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "release_port", Description: "Release a reserved port."}, releaseHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "list_reservations", Description: "List current port reservations, marking expired ones."}, listHandler(st))
	mcp.AddTool(server, &mcp.Tool{Name: "reap", Description: "Reclaim expired and dead-process reservations now."}, reapHandler(st))

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("portwarden: %v", err)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./portwarden/... -count=1`
Expected: PASS (all store tests + handler tests).

- [ ] **Step 6: Build and smoke-test the binary**

Run:
```bash
make build
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' | ./bin/portwarden
```
Expected: a single JSON-RPC response line containing `"serverInfo"` with `"name":"portwarden"` (the server reads stdin; it will block after the response — Ctrl-C to exit).

- [ ] **Step 7: Write the README**

Create `portwarden/README.md`:

```markdown
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
```

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum portwarden/main.go portwarden/main_test.go portwarden/README.md
git commit -m "feat(portwarden): wire MCP server, tool handlers, and docs"
```

---

### Task 7: Cross-box installer (`install.sh`)

**Files:**
- Create: `portwarden/install.sh`
- Modify: `portwarden/README.md` (add install one-liner)

**Interfaces:**
- Consumes: the `Makefile` `build` target (Task 1), the built binary path `bin/portwarden`.
- Produces: an idempotent installer that clones/updates the repo, builds, installs the binary to `~/.local/bin`, and registers the MCP server. Usable both `curl | bash` and from a local checkout.

Rationale: the repo is private, so a build-from-source installer (Go is present on dev boxes) over SSH/`gh` avoids release-artifact and token-gated-raw-URL friction. Prebuilt release binaries are a possible later enhancement, out of scope here.

- [ ] **Step 1: Write the installer**

Create `portwarden/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO_SLUG="randybias/randyb-dev-apps"
REPO_SSH="git@github.com:${REPO_SLUG}.git"
INSTALL_DIR="${PORTWARDEN_SRC_DIR:-${HOME}/.local/share/randyb-dev-apps}"
BIN_DIR="${PORTWARDEN_BIN_DIR:-${HOME}/.local/bin}"
APP="portwarden"

log() { printf '[install] %s\n' "$*"; }
die() { printf '[install] error: %s\n' "$*" >&2; exit 1; }

require() {
  command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

clone_or_update() {
  if [ -d "${INSTALL_DIR}/.git" ]; then
    log "updating ${INSTALL_DIR}"
    git -C "${INSTALL_DIR}" pull --ff-only
  else
    log "cloning ${REPO_SLUG} into ${INSTALL_DIR}"
    mkdir -p "$(dirname "${INSTALL_DIR}")"
    if command -v gh >/dev/null 2>&1; then
      gh repo clone "${REPO_SLUG}" "${INSTALL_DIR}"
    else
      git clone "${REPO_SSH}" "${INSTALL_DIR}"
    fi
  fi
}

build_app() {
  log "building ${APP}"
  ( cd "${INSTALL_DIR}" && make build APP="${APP}" )
}

install_bin() {
  mkdir -p "${BIN_DIR}"
  install -m 0755 "${INSTALL_DIR}/bin/${APP}" "${BIN_DIR}/${APP}"
  log "installed ${BIN_DIR}/${APP}"
}

register_mcp() {
  if command -v claude >/dev/null 2>&1; then
    log "registering ${APP} MCP server (user scope)"
    claude mcp add "${APP}" --scope user -- "${BIN_DIR}/${APP}" 2>/dev/null \
      || log "claude mcp add skipped (already registered?)"
  else
    log "claude CLI not found; register manually: claude mcp add ${APP} -- ${BIN_DIR}/${APP}"
  fi
}

main() {
  require git
  require go
  if [ -f "./${APP}/main.go" ] && [ -f "./go.mod" ]; then
    INSTALL_DIR="$(pwd)"
    log "using current checkout at ${INSTALL_DIR}"
  else
    clone_or_update
  fi
  build_app
  install_bin
  register_mcp
  log "done. ensure ${BIN_DIR} is on your PATH."
}

main "$@"
```

- [ ] **Step 2: Lint and mark executable**

Run:
```bash
shellcheck portwarden/install.sh
chmod +x portwarden/install.sh
```
Expected: shellcheck reports no warnings.

- [ ] **Step 3: Verify the local-checkout path works end to end**

Run (from repo root):
```bash
PORTWARDEN_BIN_DIR="$(mktemp -d)" bash portwarden/install.sh
```
Expected: builds, prints `installed .../portwarden`, and (if `claude` is present) registers the server. No errors.

- [ ] **Step 4: Add the install one-liner to the README**

Insert under the `## Build` section of `portwarden/README.md`:

```markdown
## Install (any dev box)

From a local checkout:

    bash portwarden/install.sh

Remote bootstrap (private repo — uses your gh auth):

    gh api repos/randybias/randyb-dev-apps/contents/portwarden/install.sh \
      -H "Accept: application/vnd.github.raw" | bash

The installer clones/updates the repo to `~/.local/share/randyb-dev-apps`,
builds, installs `portwarden` to `~/.local/bin`, and registers it as a
user-scope MCP server. Override with `PORTWARDEN_SRC_DIR` / `PORTWARDEN_BIN_DIR`.
```

- [ ] **Step 5: Commit**

```bash
git add portwarden/install.sh portwarden/README.md
git commit -m "feat(portwarden): add cross-box build-from-source installer"
```

---

## Self-Review Notes

- **Spec coverage:** architecture (Task 2/6), MCP SDK (Task 6), state store + flock + atomic write + corruption handling (Task 2), reservation record (Task 2), lease/allocation model 3 + range default (Task 4), lazy reap + PID death (Task 3, swept in 4/5), 5-tool surface (Task 6), repo layout + module path (Task 1), TDD tests (every task), Makefile (Task 1, per user request), cross-box installer (Task 7, per user request), out-of-scope items untouched. All covered.
- **PID-alive note:** `pidAlive` uses `syscall.Kill(pid, 0)` — unix only, matching the Linux/macOS-only constraint.
- **Test isolation:** `internal/store` tests inject `Now` and `PIDAlive`, so they are deterministic and need no real processes or clock.
- **Bounded tests:** all runs scoped to `./portwarden/...` with `-count=1`; Go's test runner does not fork worker storms.
