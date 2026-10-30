package logging_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guilherme-grimm/graph-go/internal/logging"
)

// captureStderr runs fn with os.Stderr redirected; zap opens its "stderr"
// sink when the logger is built, so fn must build the logger itself.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	w.Close()
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestStacktraceOnlyOnErrors(t *testing.T) {
	for _, format := range []string{"console", "json"} {
		t.Run(format, func(t *testing.T) {
			out := captureStderr(t, func() {
				l, err := logging.New("info", format)
				require.NoError(t, err)
				l.Warnw("routine warning")
				l.Errorw("real error")
				_ = l.Sync()
			})

			warn, errLine, ok := strings.Cut(out, "real error")
			require.True(t, ok, "error line missing:\n%s", out)
			assert.NotContains(t, warn, "logging_test.TestStacktraceOnlyOnErrors", "warning carries a stack trace")
			assert.Contains(t, errLine, "logging_test.TestStacktraceOnlyOnErrors", "error lost its stack trace")
		})
	}
}

// Tests run with stderr on a pipe, like docker logs or an MCP client.
func TestNoColourWhenStderrIsNotATerminal(t *testing.T) {
	out := captureStderr(t, func() {
		l, err := logging.New("info", "console")
		require.NoError(t, err)
		l.Warnw("piped")
		_ = l.Sync()
	})
	assert.Contains(t, out, "WARN")
	assert.NotContains(t, out, "\x1b[", "escape codes would show up as text")
}
