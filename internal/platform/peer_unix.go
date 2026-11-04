//go:build linux || darwin

package platform

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

func CheckPeer(conn net.Conn) error {
	peer, ok := conn.(syscall.Conn)
	if !ok {
		return fmt.Errorf("connection does not expose peer credentials")
	}
	raw, err := peer.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var peerErr error
	if err = raw.Control(func(fd uintptr) { uid, peerErr = peerUID(int(fd)) }); err != nil {
		return err
	}
	if peerErr != nil {
		return peerErr
	}
	if uid != uint32(os.Getuid()) {
		return fmt.Errorf("control peer belongs to another user")
	}
	return nil
}
