package cmd

import (
	"net"
	"time"
)

// portListening returns true if addr is accepting TCP connections.
func portListening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	return false
}
