package server

import (
	"errors"
	"os"
	"testing"

	"github.com/guilherme-grimm/graph-go/internal/config"
)

func TestShouldEnableDockerForOS(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	tests := []struct {
		name    string
		cfg     *config.Config
		goos    string
		statErr error
		env     map[string]string
		want    bool
	}{
		{name: "explicit enabled wins", cfg: &config.Config{Docker: config.DockerConfig{Enabled: boolPtr(true)}}, goos: "linux", statErr: os.ErrNotExist, want: true},
		{name: "explicit disabled wins", cfg: &config.Config{Docker: config.DockerConfig{Enabled: boolPtr(false)}}, goos: "windows", want: false},
		{name: "docker host env enables discovery", cfg: &config.Config{}, goos: "linux", statErr: os.ErrNotExist, env: map[string]string{"DOCKER_HOST": "tcp://127.0.0.1:2375"}, want: true},
		{name: "windows attempts sdk ping", cfg: &config.Config{}, goos: "windows", statErr: os.ErrNotExist, want: true},
		{name: "unix socket present enables", cfg: &config.Config{}, goos: "linux", want: true},
		{name: "unix socket missing disables", cfg: &config.Config{}, goos: "linux", statErr: os.ErrNotExist, want: false},
		{name: "custom unix socket checked", cfg: &config.Config{Docker: config.DockerConfig{Socket: "/tmp/docker.sock"}}, goos: "linux", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statPath := ""
			stat := func(path string) (os.FileInfo, error) {
				statPath = path
				if tt.statErr != nil {
					return nil, tt.statErr
				}
				return nil, nil
			}
			getenv := func(key string) string {
				if tt.env == nil {
					return ""
				}
				return tt.env[key]
			}

			got := shouldEnableDockerForOS(tt.cfg, tt.goos, stat, getenv)
			if got != tt.want {
				t.Fatalf("shouldEnableDockerForOS() = %v, want %v", got, tt.want)
			}

			if tt.name == "custom unix socket checked" && statPath != "/tmp/docker.sock" {
				t.Fatalf("stat path = %q, want /tmp/docker.sock", statPath)
			}
		})
	}
}

func TestShouldEnableDockerForOSIgnoresStatWhenWindows(t *testing.T) {
	called := false
	stat := func(path string) (os.FileInfo, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	got := shouldEnableDockerForOS(&config.Config{}, "windows", stat, func(string) string { return "" })
	if !got {
		t.Fatal("expected windows auto-detection to attempt Docker discovery")
	}
	if called {
		t.Fatal("expected windows path not to use os.Stat")
	}
}
