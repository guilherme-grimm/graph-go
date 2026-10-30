package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}

			stdout, stderr, err := executeCommand(newRootCmd(), "validate", "-c", path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if stdout != "" {
				t.Errorf("stdout should be empty, got %q", stdout)
			}
			for _, sub := range tt.wantStderr {
				if !strings.Contains(stderr, sub) {
					t.Errorf("stderr missing %q\nfull stderr:\n%s", sub, stderr)
				}
			}
		})
	}
}

func TestValidateMissingFileFails(t *testing.T) {
	_, _, err := executeCommand(newRootCmd(), "validate", "-c", filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}
