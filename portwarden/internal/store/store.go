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
