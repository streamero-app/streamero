package netx

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrBlockedAddress is returned for a connection to an address outbound
// requests never go to.
var ErrBlockedAddress = errors.New("connections to this address are not allowed")

// BlockedOutbound reports addresses the server never connects to for
// administrator-configured services (*arr, Seerr, Bazarr, webhooks, single
// sign-on, imports): link-local ones (the cloud metadata service at
// 169.254.169.254, fe80::/10), unspecified and multicast addresses. Loopback
// and private LAN addresses stay allowed: those services usually run on the
// same machine or network (D-053).
func BlockedOutbound(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified()
}

// guardedDialer checks the resolved address of every connection, so neither a
// host name nor a redirect can reach a blocked address.
func guardedDialer() *net.Dialer {
	return &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || BlockedOutbound(ip) {
				return ErrBlockedAddress
			}
			return nil
		}}
}

// SafeTransport is an HTTP transport for administrator-configured services.
func SafeTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = guardedDialer().DialContext
	return tr
}
