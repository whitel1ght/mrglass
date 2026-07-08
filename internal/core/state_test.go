package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	got, err := LoadState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty map, got %v", got)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	in := map[string]Snapshot{
		"g/p!1": {Ref: "g/p!1", CI: "success", ApprovedBy: []string{"alice"}},
	}
	if err := SaveState(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := LoadState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

func TestLoadStateCorruptFileReturnsEmptyMapAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadState(path)
	if err == nil {
		t.Error("corrupt state should return an error")
	}
	if m == nil || len(m) != 0 {
		t.Errorf("corrupt state should still return an empty usable map, got %v", m)
	}
}
