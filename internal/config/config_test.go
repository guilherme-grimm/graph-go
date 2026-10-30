package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guilherme-grimm/graph-go/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600), "write config")
	return path
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string // one substring per expected problem, in order
	}{
		{
			name: "valid config",
			body: `
server:
  port: 8080
connections:
  - name: db
    type: postgres
    dsn: postgres://localhost/db
  - name: cache
    type: redis
    uri: redis://localhost:6379
`,
		},
		{
			name: "empty file",
			body: "",
		},
		{
			name: "missing required field names field and service",
			body: `
connections:
  - name: db
    type: postgres
`,
			want: []string{"(db): 'dsn' is required for postgres"},
		},
		{
			name: "unknown type",
			body: `
connections:
  - name: queue
    type: kafka
`,
			want: []string{`(queue): unknown type "kafka"`},
		},
		{
			name: "unknown key is reported, not dropped",
			body: `
connections:
  - name: cache
    type: redis
    host: localhost
    port: 6379
`,
			want: []string{
				"line 5: unknown key 'host'",
				"line 6: unknown key 'port'",
				"(cache): 'uri' or 'endpoint' is required for redis",
			},
		},
		{
			name: "every problem is reported",
			body: `
connections:
  - type: postgres
    dsn: postgres://localhost/db
  - name: search
  - name: api
    type: http
`,
			want: []string{
				"connections[0]: 'name' is required",
				"connections[1] (search): 'type' is required",
				"connections[2] (api): 'endpoint' is required for http",
			},
		},
		{
			name: "second document is reported",
			body: `
connections:
  - name: db
    type: postgres
    dsn: postgres://localhost/db
---
connections:
  - name: cache
    type: redis
    uri: redis://localhost:6379
`,
			want: []string{"only one YAML document is supported"},
		},
		{
			name: "leading separator is a single document",
			body: `---
connections:
  - name: db
    type: postgres
    dsn: postgres://localhost/db
`,
		},
		{
			name: "trailing empty documents are fine",
			body: "connections: []\n---\n# only a comment\n---\n",
		},
		{
			name: "syntax error in a later document is reported",
			body: "connections: []\n---\nconnections: [\n",
			want: []string{"after the first document: yaml: line 3"},
		},
		{
			name: "syntax error stops validation",
			body: "connections: [\n",
			want: []string{"failed to parse"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, err := config.Check(writeConfig(t, tt.body))
			require.NoError(t, err, "Check")
			require.Len(t, problems, len(tt.want), "problems: %v", problems)
			for i, sub := range tt.want {
				assert.ErrorContains(t, problems[i], sub, "problem[%d]", i)
			}
		})
	}
}

func TestCheckMissingFile(t *testing.T) {
	_, err := config.Check(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err, "missing file")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorContains(t, err, "failed to read")
}

// Every config the repo ships must pass the strict check, so the examples
// people copy never contain keys graph-go ignores.
func TestShippedConfigsAreValid(t *testing.T) {
	// conf/config.yaml is gitignored (it's the user's own), so list explicitly.
	paths := []string{
		"../../conf/config.sample.yaml",
		"../../conf/config.docker.yaml",
		"../../hack/scale/scale.yaml",
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			problems, err := config.Check(path)
			require.NoError(t, err, "Check")
			assert.Empty(t, problems)
		})
	}
}

func TestLoadReportsAllProblems(t *testing.T) {
	path := writeConfig(t, `
connections:
  - name: db
    type: postgres
  - name: docs
    type: mongodb
`)
	_, err := config.Load(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "'dsn' is required")
	assert.ErrorContains(t, err, "'uri' is required")
}
