package middleware

import (
	"net"
	"net/http"
	"strings"
)

// RealIP is the address of the client that made the request, for the access log and for whatever limits a client
// by its address.
//
// The address of the connection is that of the proxy when there is one, so the header X-Forwarded-For is read
// as well, but not believed from just anyone: a client can write whatever it likes in it, and would pick its
// own address and walk around anything keyed on it, such as the invite code's rate limit. The header is believed
// only as far as the connection comes from a proxy on loopback or a private network (a reverse proxy on the same
// host or Docker network): the proxies, those addresses, are skipped from the right, and the first address that is
// not one of them is the client. That is the address the nearest proxy saw, which no one in front of it could
// choose. The reverse proxy has to put the client's address in that header.
//
// The addresses it will skip are loopback, link-local and private ones. An unparseable entry in the header makes
// the whole of it untrustworthy, and the address of the connection is returned. If every address in the chain is
// one of the proxies' own, the first is returned, which is the furthest one from the server.
func RealIP(r *http.Request) string {
	direct := directIP(r)

	forwarded := r.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return direct
	}

	ips := append(strings.Split(strings.Join(forwarded, ","), ","), direct)
	for i := len(ips) - 1; i >= 0; i-- {
		ips[i] = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ips[i]), "["), "]")
		ip := net.ParseIP(ips[i])
		if ip == nil {
			return direct
		}
		if !trusted(ip) {
			return ip.String()
		}
	}

	return ips[0]
}

// directIP is the address of the connection, with no port.
func directIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if net.ParseIP(r.RemoteAddr) != nil {
			return r.RemoteAddr
		}

		return ""
	}

	return host
}

// trusted reports whether ip is the address of a proxy that is believed about the client behind it.
func trusted(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()
}
