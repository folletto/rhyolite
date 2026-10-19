package server

import (
	"net"
	"strings"
)

// LocalOnly wraps ln so that it hands out only connections from this machine
// (see localConn). Any other connection is closed before an HTTP request is
// read, and each refused IP is logged once as "refused". The wrapper keeps the
// bind address of ln, so the machine's own names keep working even where they
// resolve to its LAN addresses. Addr and Close come from ln. A nil log discards
// the events.
func LocalOnly(ln net.Listener, log Logger) net.Listener {
	if log == nil {
		log = nopLogger{}
	}
	return &localListener{Listener: ln, log: log}
}

type localListener struct {
	net.Listener
	log     Logger
	refused clientTracker // IPs already logged as refused
}

// Accept returns the next connection from this machine. Connections from
// elsewhere are closed and skipped; an error from the listener is returned.
func (l *localListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if localConn(conn.RemoteAddr(), conn.LocalAddr()) {
			return conn, nil
		}
		ip := remoteIP(conn.RemoteAddr())
		conn.Close()
		if l.refused.isNew(ip) {
			l.log.Log("refused", "ip", ip)
		}
	}
}

// localConn reports whether a connection comes from this machine: its remote IP
// is loopback, or equal to the local IP of the connection. A process on this
// machine that connects to one of the machine's own addresses (for example via
// a .local name that resolves to a LAN address) is given that same address as
// its source. A remote host cannot complete a TCP handshake using one of this
// machine's addresses as its source, so a match means the connection is from
// here. IPv6 zones are ignored and IPv4-mapped addresses equal their IPv4 form.
// An address that cannot be read makes the connection non-local.
func localConn(remote, local net.Addr) bool {
	r, l := addrIP(remote), addrIP(local)
	if r == nil || l == nil {
		return false
	}
	return r.IsLoopback() || r.Equal(l)
}

// remoteIP returns the IP of a remote address as text, for the log.
func remoteIP(remote net.Addr) string {
	if ip := addrIP(remote); ip != nil {
		return ip.String()
	}
	return "?"
}

// addrIP returns the IP of an address, or nil if it has none. A *net.TCPAddr is
// used as it is; any other address is parsed from its text.
func addrIP(a net.Addr) net.IP {
	if t, ok := a.(*net.TCPAddr); ok && t != nil {
		return t.IP
	}
	if a == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return nil
	}
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return net.ParseIP(host)
}
