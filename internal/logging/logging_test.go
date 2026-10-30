package logging_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/guilherme-grimm/graph-go/internal/logging"
)

// captureStderr runs fn with os.Stderr redirected; zap opens its "stderr"
// sink when the logger is built, so fn must build the logger itself.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestStacktraceOnlyOnErrors(t *testing.T) {
	for _, format := range []string{"console", "json"} {
		t.Run(format, func(t *testing.T) {
			out := captureStderr(t, func() {
				l, err := logging.New("info", format)
				if err != nil {
					t.Fatal(err)
				}
				l.Warnw("routine warning")
				l.Errorw("real error")
				_ = l.Sync()
			})

			warn, errLine, ok := strings.Cut(out, "real error")
			if !ok {
				t.Fatalf("error line missing:\n%s", out)
			}
			if strings.Contains(warn, "logging_test.TestStacktraceOnlyOnErrors") {
				t.Errorf("warning carries a stack trace:\n%s", warn)
			}
			if !strings.Contains(errLine, "logging_test.TestStacktraceOnlyOnErrors") {
				t.Errorf("error lost its stack trace:\n%s", errLine)
			}
		})
	}
}
