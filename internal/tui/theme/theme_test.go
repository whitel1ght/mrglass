package theme

import (
	"testing"

	"github.com/dmitry/mrglass/internal/config"
)

func TestGetUnknownFallsBackToDefault(t *testing.T) {
	got := Get("does-not-exist")
	if got.Name != "default" {
		t.Errorf("unknown theme should fall back to default, got %q", got.Name)
	}
}

func TestRegistryHasDefaultAndDracula(t *testing.T) {
	r := Registry()
	if _, ok := r["default"]; !ok {
		t.Error("registry missing default")
	}
	if _, ok := r["dracula"]; !ok {
		t.Error("registry missing dracula")
	}
}

func TestStyleFromAppliesBold(t *testing.T) {
	s := StyleFrom(config.StyleConfig{FG: "#ffffff", Bold: true})
	if !s.GetBold() {
		t.Error("expected bold style")
	}
}

func TestBuildStylesDoesNotPanic(t *testing.T) {
	_ = BuildStyles(Get("default"))
}
