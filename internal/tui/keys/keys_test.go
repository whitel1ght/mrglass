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

func TestOpenTicketBinding(t *testing.T) {
	k := Default()
	if len(k.OpenTicket.Keys()) == 0 {
		t.Error("OpenTicket must have a key bound")
	}
	if k.OpenTicket.Keys()[0] != "J" {
		t.Errorf("OpenTicket should be J, got %v", k.OpenTicket.Keys())
	}
}

func TestOpenWorkBinding(t *testing.T) {
	k := Default()
	if len(k.OpenWork.Keys()) == 0 || k.OpenWork.Keys()[0] != "w" {
		t.Errorf("OpenWork should be w, got %v", k.OpenWork.Keys())
	}
}

func TestShortHelpFitsNarrow(t *testing.T) {
	sh := Default().ShortHelp()
	var w int
	for _, b := range sh {
		h := b.Help()
		w += len(h.Key) + 1 + len(h.Desc) + 3 // key + space + desc + separator
	}
	if w > 60 {
		t.Errorf("short help ~%d cols; must stay compact (<=60) so it never crops", w)
	}
	if len(sh) < 4 {
		t.Errorf("short help should still surface the core keys, got %d", len(sh))
	}
}
