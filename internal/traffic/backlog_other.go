//go:build !linux

package traffic

import "net"

// Automatic Docker activation is Linux-only; retain portable unit-test builds.
func boundTCPBacklog(*net.TCPListener) error { return nil }
