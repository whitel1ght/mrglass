package review

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// logPath returns the review debug-log location (under the user's cache dir,
// falling back to the temp dir).
func logPath() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "mrglass", "review.log")
	}
	return filepath.Join(os.TempDir(), "mrglass-review.log")
}

// LogPath is the human-facing path to the review log (for status messages).
func LogPath() string { return logPath() }

// logf appends a timestamped line to the review log, best-effort. Used to record
// the FULL error text (the status bar truncates), so failures are diagnosable.
// stamp is passed in because time.Now is fine here (not in the workflow sandbox).
func logf(format string, args ...any) {
	p := logPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
