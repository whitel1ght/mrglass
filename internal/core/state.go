package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadState reads a {ref: Snapshot} map. A missing file yields an empty map and
// no error (first run). A corrupt file yields an empty map AND an error — the
// caller can proceed as first-run but should surface the warning.
func LoadState(path string) (map[string]Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Snapshot{}, nil
		}
		return nil, err
	}
	var m map[string]Snapshot
	if err := json.Unmarshal(b, &m); err != nil {
		// Corrupt file: return a usable empty baseline plus the error, so the
		// caller can proceed as first-run while telling the user their change
		// history was lost.
		return map[string]Snapshot{}, fmt.Errorf("state file %s is corrupt (%v); treating as first run", path, err)
	}
	return m, nil
}

// SaveState writes the snapshot map as indented JSON, creating parent dirs.
func SaveState(path string, snaps map[string]Snapshot) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(snaps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
