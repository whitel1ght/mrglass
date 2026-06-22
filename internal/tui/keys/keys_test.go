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
