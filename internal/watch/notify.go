package watch

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/dmitry/mrglass/internal/core"
)

// NotifyText formats the notification title and body for a change.
func NotifyText(c core.Change) (title, body string) {
	return fmt.Sprintf("MR %s", c.Ref), fmt.Sprintf("%s — %s", c.Title, c.Detail)
}

// Notify fires a best-effort desktop notification. It never errors and no-ops
// when no backend is available.
func Notify(c core.Change) {
	title, body := NotifyText(c)
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		_ = exec.Command("osascript", "-e", script).Run()
	default:
		if _, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command("notify-send", title, body).Run()
		}
	}
}
