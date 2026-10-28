//go:build unix

package docker

import (
	"io/fs"
	"os"
	"syscall"
)

// SocketGID returns the group id that owns the Docker socket at path.
func SocketGID(path string) (int, bool) {
	gid, _, ok := socketOwnership(path)
	return gid, ok
}

// socketOwnership returns the socket's owning group and permission bits.
func socketOwnership(path string) (int, fs.FileMode, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Gid), fi.Mode().Perm(), true
}
