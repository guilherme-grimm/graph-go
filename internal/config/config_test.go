package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guilherme-grimm/graph-go/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
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
			name: "syntax error stops validation",
			body: "connections: [\n",
			want: []string{"failed to parse"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, err := config.Check(writeConfig(t, tt.body))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if len(problems) != len(tt.want) {
				t.Fatalf("got %d problems, want %d: %v", len(problems), len(tt.want), problems)
			}
			for i, sub := range tt.want {
				if !strings.Contains(problems[i].Error(), sub) {
					t.Errorf("problem[%d] = %q, want it to contain %q", i, problems[i], sub)
				}
			}
		})
	}
}

func TestCheckMissingFile(t *testing.T) {
	_, err := config.Check(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("unexpected error: %v", err)
	}
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
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			for _, p := range problems {
				t.Error(p)
			}
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
	if err == nil {
		t.Fatal("expected error")
	}
	for _, sub := range []string{"'dsn' is required", "'uri' is required"} {
		if !strings.Contains(err.Error(), sub) {
			t.Errorf("error %q missing %q", err, sub)
		}
	}
}
