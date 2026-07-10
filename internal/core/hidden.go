package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HiddenPath derives the hidden-refs file path from the snapshot state path,
// e.g. ".mrglass-state.json" → ".mrglass-state-hidden.json".
func HiddenPath(statePath string) string {
	ext := filepath.Ext(statePath)
	return strings.TrimSuffix(statePath, ext) + "-hidden" + ext
}

// LoadHidden reads the set of user-hidden MR refs. A missing file yields an
// empty set and no error. A corrupt file yields an empty usable set AND an
// error the caller may surface.
func LoadHidden(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return map[string]bool{}, err
	}
	var refs []string
	if err := json.Unmarshal(b, &refs); err != nil {
		return map[string]bool{}, fmt.Errorf("hidden file %s is corrupt (%v); ignoring it", path, err)
	}
	set := make(map[string]bool, len(refs))
	for _, r := range refs {
		set[r] = true
	}
	return set, nil
}

// SaveHidden writes the hidden refs as a sorted JSON array (stable diffs),
// creating parent dirs.
func SaveHidden(path string, refs map[string]bool) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	list := make([]string, 0, len(refs))
	for r := range refs {
		list = append(list, r)
	}
	sort.Strings(list)
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
