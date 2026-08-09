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
	if err1 != nil || err2 != nil || low < minPort || high > maxPort || high < low {
		return defLow, defHigh
	}
	return low, high
}
