package traffic

import (
	"golang.org/x/sys/unix"
	"net"
)

func boundTCPBacklog(socket *net.TCPListener) error {
	connection, err := socket.SyscallConn()
	if err != nil {
		return err
	}
	var listenErr error
	if err := connection.Control(func(fd uintptr) { listenErr = unix.Listen(int(fd), 16) }); err != nil {
		return err
	}
	return listenErr
}
