//go:build !unix

package docker

import "io/fs"

// SocketGID reports false on platforms without Unix socket ownership
// (Windows reaches the daemon over a named pipe).
func SocketGID(string) (int, bool) { return 0, false }

func socketOwnership(string) (int, fs.FileMode, bool) { return 0, 0, false }
