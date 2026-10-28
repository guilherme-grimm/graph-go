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
	gid, mode, ok := socketOwnership(socket)
	return socketHint(os.Getuid(), socket, gid, mode, ok)
}

// socketHint words the hint for a process (uid) that could not open socket.
// Group membership only helps when the socket grants its group read+write,
// so a socket like 0600 gets different advice than the usual 0660.
func socketHint(uid int, socket string, gid int, mode fs.FileMode, known bool) string {
	if !known {
		return fmt.Sprintf("uid %d cannot open %s: check that the socket exists and its permissions, then run the container with --group-add <gid of %s> (compose: group_add)", uid, socket, socket)
	}
	if mode&0o060 != 0o060 {
		return fmt.Sprintf("uid %d cannot open %s: its mode %#o gives group %d no read/write access, so --group-add will not help; loosen the socket permissions or reach Docker through a socket proxy (DOCKER_HOST)", uid, socket, mode, gid)
	}
	return fmt.Sprintf("uid %d cannot open %s (group %d, mode %#o): if the process is not in that group, add --group-add %d to docker run, or group_add: [\"%d\"] in compose; otherwise check the socket's permissions", uid, socket, gid, mode, gid, gid)
}
