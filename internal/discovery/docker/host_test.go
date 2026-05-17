package docker

import "testing"

func TestDockerHostFromSocket(t *testing.T) {
	tests := []struct {
		name   string
		socket string
		goos   string
		want   string
	}{
		{name: "unix socket", socket: "/var/run/docker.sock", goos: "linux", want: "unix:///var/run/docker.sock"},
		{name: "existing tcp scheme preserved", socket: "tcp://127.0.0.1:2375", goos: "linux", want: "tcp://127.0.0.1:2375"},
		{name: "existing npipe scheme preserved", socket: "npipe:////./pipe/docker_engine", goos: "windows", want: "npipe:////./pipe/docker_engine"},
		{name: "windows named pipe", socket: `\\.\pipe\docker_engine`, goos: "windows", want: "npipe:////./pipe/docker_engine"},
		{name: "windows non-pipe custom host falls back to unix compatibility", socket: "/var/run/docker.sock", goos: "windows", want: "unix:///var/run/docker.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dockerHostFromSocket(tt.socket, tt.goos); got != tt.want {
				t.Fatalf("dockerHostFromSocket(%q, %q) = %q, want %q", tt.socket, tt.goos, got, tt.want)
			}
		})
	}
}
