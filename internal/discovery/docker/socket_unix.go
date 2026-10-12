//go:build unix

package docker

import (
	"os"
	"syscall"
)

// SocketGID returns the group id that owns the Docker socket at path.
func SocketGID(path string) (int, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Gid), true
}
