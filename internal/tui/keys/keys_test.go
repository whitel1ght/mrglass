package keys

import "testing"

func TestDefaultBindingsHaveKeys(t *testing.T) {
	k := Default()
	if len(k.Quit.Keys()) == 0 {
		t.Error("Quit must have keys bound")
	}
	if len(k.Open.Keys()) == 0 {
		t.Error("Open must have keys bound")
	}
}

func TestShortHelpNonEmpty(t *testing.T) {
	if len(Default().ShortHelp()) == 0 {
		t.Error("ShortHelp should list bindings")
	}
}
