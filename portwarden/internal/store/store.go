package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	PortFree   func(int) bool
}

// Store performs lock-protected reservation operations against the state file.
type Store struct {
	cfg     Config
	pathErr error // why the default state path could not be resolved, if it couldn't
}

const (
	defaultRangeLow  = 20000
	defaultRangeHigh = 29999
	defaultTTL       = 30 * 24 * time.Hour
	minPort          = 1
	maxPort          = 65535
)

// New returns a Store, filling unset Config fields with defaults (including
// env-driven port range and the resolved state path).
// A state path that cannot be resolved is reported by every operation rather
// than letting the store fall back to the working directory.
func New(cfg Config) *Store {
	var pathErr error
	if cfg.Path == "" {
		if p, err := StatePath(); err != nil {
			pathErr = fmt.Errorf("resolve state path: %w", err)
		} else {
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
	if cfg.PortFree == nil {
		cfg.PortFree = portFree
	}
	return &Store{cfg: cfg, pathErr: pathErr}
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

// probeAddrs are the addresses portFree tries to bind. Both wildcards and both
// loopbacks are needed: Go sets SO_REUSEADDR, and on BSD/macOS that lets a
// wildcard bind succeed while a listener holds the same port on 127.0.0.1, so
// probing only the wildcard reports a busy port as free. Binding the identical
// address always conflicts, so the loopback probes catch what the wildcards
// miss. A listener on some other specific interface is not detected, which is
// an accepted limit for a local dev-port arbiter.
var probeAddrs = []struct{ network, host string }{
	{"tcp4", "0.0.0.0"},
	{"tcp4", "127.0.0.1"},
	{"tcp6", "[::]"},
	{"tcp6", "[::1]"},
}

// familyUnavailable reports whether a listen error means this host simply does
// not offer that address family -- the only reason to disregard a probe rather
// than let it condemn the port.
func familyUnavailable(err error) bool {
	return errors.Is(err, syscall.EAFNOSUPPORT) ||
		errors.Is(err, syscall.EPFNOSUPPORT) ||
		errors.Is(err, syscall.EADDRNOTAVAIL)
}

// portFree reports whether a TCP port can actually be bound right now. The
// ledger only knows what sessions chose to tell it; a process that outlived its
// lease, or never took one, is invisible there. EADDRINUSE means the lane is
// taken. Anything else unexpected -- EACCES on a privileged port, EMFILE under
// descriptor pressure, a malformed address -- also returns false, because a
// probe that could not run is not evidence the port is usable, and handing out
// an unbindable port is the exact failure this function exists to prevent. Only
// a genuinely absent address family is disregarded, and at least one bind must
// succeed. SO_REUSEADDR means a socket lingering in TIME_WAIT does not read as
// in use, while a live listener does.
//
// This is a snapshot, not a lock. The probe closes each listener before Reserve
// records the lane, so an uncoordinated process can still take the port in
// between; callers must handle bind failure rather than trust the reservation.
func portFree(port int) bool {
	if port < minPort || port > maxPort {
		return false
	}
	bound := 0
	for _, a := range probeAddrs {
		ln, err := net.Listen(a.network, a.host+":"+strconv.Itoa(port))
		if err != nil {
			if familyUnavailable(err) {
				continue
			}
			return false
		}
		ln.Close()
		bound++
	}
	return bound > 0
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

// saveState writes atomically and durably: temp file in the same dir, fsync,
// rename, then fsync the dir. Without the syncs a crash can leave a renamed but
// empty reservations.json, which loadState would read as a fresh ledger.
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp: %w", err)
	}
	return syncDir(dir)
}

// syncDir makes a rename within dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open state dir: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync state dir: %w", err)
	}
	return nil
}

// ReserveRequest describes a reservation. Port 0 means auto-allocate; TTL 0
// means use the configured default.
type ReserveRequest struct {
	Owner   string
	Purpose string
	Port    int
	TTL     time.Duration
	PID     int
	Force   bool
}

// Reserve allocates a port lane. It sweeps stale leases first, then honors a
// requested port (error if taken) or allocates the lowest free port in range.
// "Free" means free in both senses: unclaimed in the ledger and not currently
// bound by a process.
func (s *Store) Reserve(req ReserveRequest) (Reservation, error) {
	if req.Owner == "" {
		return Reservation{}, errors.New("owner is required")
	}
	if req.Port != 0 && (req.Port < minPort || req.Port > maxPort) {
		return Reservation{}, fmt.Errorf("port %d is out of range (%d-%d)", req.Port, minPort, maxPort)
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
			if !req.Force && !s.cfg.PortFree(port) {
				return false, fmt.Errorf("port %d is in use by a process with no reservation; "+
					"stop it, pick another port, or pass force to claim the lane anyway", port)
			}
		} else {
			for p := s.cfg.RangeLow; p <= s.cfg.RangeHigh; p++ {
				if _, taken := used[p]; taken {
					continue
				}
				if !s.cfg.PortFree(p) {
					continue
				}
				port = p
				break
			}
			if port == 0 {
				return false, fmt.Errorf("no free ports in range %d-%d (every port is either reserved or in use)",
					s.cfg.RangeLow, s.cfg.RangeHigh)
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

// withLock acquires an advisory lock around a load -> fn -> (conditional) save
// cycle. exclusive=false takes a shared lock and never saves.
func (s *Store) withLock(exclusive bool, fn func(st *state) (changed bool, err error)) error {
	if s.pathErr != nil {
		return s.pathErr
	}
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
