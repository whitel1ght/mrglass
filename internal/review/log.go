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

// logSink writes one formatted log line. Defaults to the file appender; tests
// swap it to capture output.
var logSink = appendToFile

// Logf is the exported logger so other packages (e.g. the TUI) can record
// review/post outcomes to the same review log.
func Logf(format string, args ...any) { logf(format, args...) }

// logf records a timestamped line about a review (both successes — for an audit
// trail of which skill ran — and failures, with the full untruncated error).
func logf(format string, args ...any) {
	logSink(fmt.Sprintf(format, args...))
}

func appendToFile(line string) {
	p := logPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}
