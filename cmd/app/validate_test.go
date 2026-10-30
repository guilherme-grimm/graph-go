package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantErr    bool
		wantStderr []string
	}{
		{
			name: "valid config exits zero",
			body: `
connections:
  - name: db
    type: postgres
    dsn: postgres://localhost/db
`,
			wantStderr: []string{"✓", "is valid"},
		},
		{
			name: "invalid config exits non-zero and lists every problem",
			body: `
connections:
  - name: db
    type: postgres
  - name: queue
    type: kafka
`,
			wantErr: true,
			wantStderr: []string{
				"2 problem(s)",
				"(db): 'dsn' is required for postgres",
				`(queue): unknown type "kafka"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))

			stdout, stderr, err := executeCommand(newRootCmd(), "validate", "-c", path)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Empty(t, stdout, "stdout should stay empty")
			for _, sub := range tt.wantStderr {
				assert.Contains(t, stderr, sub)
			}
		})
	}
}

func TestValidateMissingFileFails(t *testing.T) {
	_, _, err := executeCommand(newRootCmd(), "validate", "-c", filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err, "missing config file")
}
