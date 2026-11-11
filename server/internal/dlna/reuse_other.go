//go:build !unix

package dlna

import "syscall"

func reuseAddr(_, _ string, _ syscall.RawConn) error { return nil }
