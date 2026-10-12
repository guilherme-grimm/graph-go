package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SocketAccessHint explains how to fix a permission-denied connection to the
// Docker socket, or returns "" when err is not a permission error.
//
// The published image runs as a non-root user, so it can only reach the socket
// if it is a member of the socket's group. That group differs per host (the
// `docker` group's gid on Linux), which is why the fix is a runtime flag rather
// than something the image can bake in.
func SocketAccessHint(socket string, err error) string {
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return ""
	}
	gid, ok := SocketGID(socket)
	if !ok {
		return fmt.Sprintf("uid %d cannot open %s: run the container with --group-add <gid of %s> (compose: group_add)", os.Getuid(), socket, socket)
	}
	return fmt.Sprintf("uid %d is not in group %d, which owns %s: add --group-add %d to docker run, or group_add: [\"%d\"] in compose", os.Getuid(), gid, socket, gid, gid)
}
