//go:build !unix

package docker

// SocketGID reports false on platforms without Unix socket ownership
// (Windows reaches the daemon over a named pipe).
func SocketGID(string) (int, bool) { return 0, false }
