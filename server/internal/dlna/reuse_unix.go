//go:build unix

package dlna

import (
	"syscall"
)

// reuseAddr lets several programs on one host share UDP 1900 (SO_REUSEADDR),
// as multicast listeners must.
func reuseAddr(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1) // #nosec G115 -- fd fits int
	})
	if err != nil {
		return err
	}
	return serr
}
