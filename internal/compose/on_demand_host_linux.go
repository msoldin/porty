//go:build linux

package compose

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// The native process must share dockerd's network namespace. Verify the local
// socket peer, rather than trusting daemon names or environment variables.
func verifyNativeDockerNetwork(ctx context.Context, path string) error {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return ErrProtectionUnavailable
	}
	defer connection.Close()
	socket, ok := connection.(*net.UnixConn)
	if !ok {
		return ErrProtectionUnavailable
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return ErrProtectionUnavailable
	}
	var credentials *unix.Ucred
	var credentialErr error
	err = raw.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil || credentialErr != nil || credentials == nil || credentials.Pid <= 0 || credentials.Uid != 0 {
		return ErrProtectionUnavailable
	}
	process := "/proc/" + strconv.Itoa(int(credentials.Pid))
	command, err := os.ReadFile(process + "/comm")
	if err != nil {
		return ErrProtectionUnavailable
	}
	pid := int(credentials.Pid)
	if pid == 1 && strings.TrimSpace(string(command)) == "systemd" && (path == "/var/run/docker.sock" || path == "/run/docker.sock") {
		// With socket activation the listening socket belongs to systemd.
		// Resolve dockerd through its root-owned default PID file instead.
		pid, err = trustedDockerPID("/run/docker.pid")
		if err != nil {
			return err
		}
	} else if strings.TrimSpace(string(command)) != "dockerd" {
		return ErrProtectionUnavailable
	}
	return socketInProcessNetwork(socket, pid)
}

func trustedDockerPID(path string) (int, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, ErrProtectionUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32 || info.Mode().Perm()&0022 != 0 {
		return 0, ErrProtectionUnavailable
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 {
		return 0, ErrProtectionUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil || len(data) > 32 {
		return 0, ErrProtectionUnavailable
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, ErrProtectionUnavailable
	}
	process := "/proc/" + strconv.Itoa(pid)
	info, err = os.Stat(process)
	if err != nil {
		return 0, ErrProtectionUnavailable
	}
	owner, ok = info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 {
		return 0, ErrProtectionUnavailable
	}
	command, err := os.ReadFile(process + "/comm")
	if err != nil || strings.TrimSpace(string(command)) != "dockerd" {
		return 0, ErrProtectionUnavailable
	}
	return pid, nil
}

// A live socket's inode cannot be reused. Finding our client socket in the
// daemon's namespace-specific socket table proves shared network visibility,
// without ptrace permissions for root-owned /proc/PID/ns symlinks. Read-only
// procfs restrictions fail closed; no namespace is entered or modified.
func socketInProcessNetwork(socket *net.UnixConn, pid int) error {
	if pid <= 0 {
		return ErrProtectionUnavailable
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return ErrProtectionUnavailable
	}
	var stat unix.Stat_t
	var statErr error
	if err := raw.Control(func(fd uintptr) { statErr = unix.Fstat(int(fd), &stat) }); err != nil || statErr != nil || stat.Ino == 0 {
		return ErrProtectionUnavailable
	}
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/net/unix")
	if err != nil {
		return ErrProtectionUnavailable
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 8<<20))
	inode := strconv.FormatUint(stat.Ino, 10)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 7 && fields[6] == inode {
			return nil
		}
	}
	return ErrProtectionUnavailable
}
