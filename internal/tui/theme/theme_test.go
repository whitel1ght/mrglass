package theme

import (
	"testing"

	"github.com/dmitry/mrglass/internal/config"
)

func TestGetUnknownFallsBackToTokyonight(t *testing.T) {
	got := Get("does-not-exist")
	if got.Name != "tokyonight" {
		t.Errorf("unknown theme should fall back to tokyonight, got %q", got.Name)
	}
}

func TestRegistryHasThemes(t *testing.T) {
	r := Registry()
	for _, name := range []string{"tokyonight", "default", "dracula"} {
		if _, ok := r[name]; !ok {
			t.Errorf("registry missing %q", name)
		}
	}
}

func TestThemesHaveAccentColor(t *testing.T) {
	for name, th := range Registry() {
		if th.Accent == "" {
			t.Errorf("theme %q missing Accent color", name)
		}
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
