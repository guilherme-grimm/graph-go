//go:build unix

package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unreachableSocket listens on a real Unix socket and strips its permissions,
// so connecting fails with EACCES exactly as a non-root container does against
// a root:docker 0660 /var/run/docker.sock.
func unreachableSocket(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses socket permissions")
	}
	path := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}

func TestNewDockerDiscovery_PermissionDeniedIsDetectable(t *testing.T) {
	sock := unreachableSocket(t)

	_, err := NewDockerDiscovery(DockerDiscoveryConfig{Socket: sock}, nil)
	if err == nil {
		t.Fatal("expected error connecting to unreadable socket")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error does not wrap fs.ErrPermission: %v", err)
	}

	hint := SocketAccessHint(sock, err)
	gid, ok := SocketGID(sock)
	if !ok {
		t.Fatal("SocketGID could not stat the socket")
	}
	if want := fmt.Sprintf("--group-add %d", gid); !strings.Contains(hint, want) {
		t.Errorf("hint %q does not contain %q", hint, want)
	}
}

func TestSocketAccessHint(t *testing.T) {
	sock := unreachableSocket(t)
	gid, _ := SocketGID(sock)

	tests := []struct {
		name   string
		socket string
		err    error
		want   string
	}{
		{name: "nil error", socket: sock, err: nil, want: ""},
		{name: "unrelated error", socket: sock, err: errors.New("connection refused"), want: ""},
		{name: "permission with known gid", socket: sock, err: fs.ErrPermission, want: fmt.Sprintf("group_add: [\"%d\"]", gid)},
		{name: "permission with missing socket", socket: filepath.Join(t.TempDir(), "nope.sock"), err: fs.ErrPermission, want: "--group-add"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SocketAccessHint(tt.socket, tt.err)
			if tt.want == "" {
				if got != "" {
					t.Errorf("want no hint, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("hint %q does not contain %q", got, tt.want)
			}
		})
	}
}
