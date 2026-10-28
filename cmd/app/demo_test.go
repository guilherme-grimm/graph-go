package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDemoCommandSurface(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T, root *cobra.Command)
	}{
		{
			name: "subcommand registered",
			assert: func(t *testing.T, root *cobra.Command) {
				if findSubcommand(root, "demo") == nil {
					t.Fatal("demo subcommand not registered on root")
				}
			},
		},
		{
			name: "inherits config",
			assert: func(t *testing.T, root *cobra.Command) {
				demoCmd := findSubcommand(root, "demo")
				if demoCmd == nil {
					t.Fatal("demo subcommand not registered")
				}
				if demoCmd.InheritedFlags().Lookup("config") == nil {
					t.Error("demo does not inherit --config")
				}
			},
		},
		{
			name: "inherits log-level",
			assert: func(t *testing.T, root *cobra.Command) {
				demoCmd := findSubcommand(root, "demo")
				if demoCmd == nil {
					t.Fatal("demo subcommand not registered")
				}
				if demoCmd.InheritedFlags().Lookup("log-level") == nil {
					t.Error("demo does not inherit --log-level")
				}
			},
		},
		{
			name: "inherits log-format",
			assert: func(t *testing.T, root *cobra.Command) {
				demoCmd := findSubcommand(root, "demo")
				if demoCmd == nil {
					t.Fatal("demo subcommand not registered")
				}
				if demoCmd.InheritedFlags().Lookup("log-format") == nil {
					t.Error("demo does not inherit --log-format")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assert(t, newRootCmd())
		})
	}
}

func TestDemoHelpOutput(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantInOut string
	}{
		{name: "demo help mentions Docker Compose", args: []string{"demo", "--help"}, wantInOut: "Docker Compose"},
		{name: "demo help mentions repo-scoped onboarding", args: []string{"demo", "--help"}, wantInOut: "repo-scoped onboarding"},
		{name: "root help lists demo", args: []string{"--help"}, wantInOut: "demo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _, err := executeCommand(newRootCmd(), tt.args...)
			if err != nil {
				t.Fatalf("executeCommand: %v", err)
			}
			if !strings.Contains(out, tt.wantInOut) {
				t.Errorf("output missing %q\nfull output:\n%s", tt.wantInOut, out)
			}
		})
	}
}

func TestDemoHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "graph-go")
	nested := filepath.Join(repoRoot, "cmd", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, path := range []string{filepath.Join(repoRoot, "go.mod"), filepath.Join(repoRoot, demoComposeFile)} {
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	tests := []struct {
		name    string
		run     func(t *testing.T) error
		want    string
		wantErr bool
	}{
		{
			name: "find repo root from nested path",
			run: func(t *testing.T) error {
				got, err := findRepoRootFrom(nested)
				if err != nil {
					return err
				}
				if got != repoRoot {
					return errors.New("unexpected repo root")
				}
				return nil
			},
		},
		{
			name: "missing repo root errors clearly",
			run: func(t *testing.T) error {
				_, err := findRepoRootFrom(t.TempDir())
				return err
			},
			want:    "repo root not found",
			wantErr: true,
		},
		{
			name: "missing docker CLI errors clearly",
			run: func(t *testing.T) error {
				originalLookPath := lookPath
				lookPath = func(file string) (string, error) {
					return "", errors.New("missing")
				}
				t.Cleanup(func() {
					lookPath = originalLookPath
				})
				return checkDockerCLI()
			},
			want:    "docker CLI not found",
			wantErr: true,
		},
		{
			name: "missing docker compose errors clearly",
			run: func(t *testing.T) error {
				originalRunDemoCheck := runDemoCheck
				runDemoCheck = func(cmd *exec.Cmd) error {
					return errors.New("compose failed")
				}
				t.Cleanup(func() {
					runDemoCheck = originalRunDemoCheck
				})
				return checkDockerCompose(t.TempDir())
			},
			want:    "docker compose",
			wantErr: true,
		},
		{
			name: "busy demo ports are reported clearly",
			run: func(t *testing.T) error {
				originalRunDemoOutput := runDemoOutput
				runDemoOutput = func(cmd *exec.Cmd) ([]byte, error) {
					return []byte("demo-a\t0.0.0.0:8080->8080/tcp\ndemo-b\t0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp\n"), nil
				}
				t.Cleanup(func() {
					runDemoOutput = originalRunDemoOutput
				})
				return checkDemoPorts(t.TempDir())
			},
			want:    "demo ports already in use: 5432 (demo-b), 8080 (demo-a)",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("error %v does not contain %q", err, tt.want)
			}
		})
	}
}

func TestParseBusyDemoPorts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[int]string
	}{
		{
			name:  "extracts published demo ports",
			input: "demo-a\t0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp\ndemo-b\t0.0.0.0:9000-9001->9000-9001/tcp\ndemo-c\t127.0.0.1:6443->6443/tcp\n",
			want: map[int]string{
				8080: "demo-a",
				9000: "demo-b",
			},
		},
		{
			name:  "ignores empty and unrelated ports",
			input: "demo-a\t\ndemo-b\t127.0.0.1:6443->6443/tcp\n",
			want:  map[int]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBusyDemoPorts(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len(parseBusyDemoPorts()) = %d, want %d", len(got), len(tt.want))
			}
			for port, wantName := range tt.want {
				if got[port] != wantName {
					t.Fatalf("parseBusyDemoPorts()[%d] = %q, want %q", port, got[port], wantName)
				}
			}
		})
	}
}
