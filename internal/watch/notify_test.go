package watch

import (
	"strings"
	"testing"

	"github.com/dmitry/mrglass/internal/core"
)

func TestNotifyText(t *testing.T) {
	c := core.Change{Ref: "g/p!1", Title: "feat: thing", Detail: "CI success → failed"}
	title, body := NotifyText(c)
	if !strings.Contains(title, "g/p!1") {
		t.Errorf("title %q should contain the ref", title)
	}
	if !strings.Contains(body, "failed") {
		t.Errorf("body %q should contain the detail", body)
	}
}
