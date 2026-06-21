package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LoadState reads a {ref: Snapshot} map. A missing or unparseable file yields an
// empty map and no error — the caller treats that as "first run / no baseline".
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
		return map[string]Snapshot{}, nil
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
