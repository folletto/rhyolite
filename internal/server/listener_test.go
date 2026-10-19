package server

import (
	"errors"
	"net"
	"testing"
	"time"
)

// tcpAddr returns the address of a TCP end at ip and port.
func tcpAddr(ip string, port int) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: port}
}

// textAddr is an address that is not a *net.TCPAddr: only its text is known.
type textAddr string

func (a textAddr) Network() string { return "test" }
func (a textAddr) String() string  { return string(a) }

func TestLocalConn(t *testing.T) {
	lan := tcpAddr("192.168.1.20", 6270)
	cases := []struct {
		name          string
		remote, local net.Addr
		want          bool
	}{
		{"IPv4 loopback", tcpAddr("127.0.0.1", 40000), lan, true},
		{"IPv6 loopback", tcpAddr("::1", 40000), lan, true},
		{"IPv4-mapped loopback", tcpAddr("::ffff:127.0.0.1", 40000), lan, true},
		{"own IPv4 address", tcpAddr("192.168.1.20", 40000), lan, true},
		{"own IPv4 address, 4-byte form", &net.TCPAddr{IP: net.IP{192, 168, 1, 20}, Port: 40000}, lan, true},
		{"own IPv4 address, IPv4-mapped form", tcpAddr("::ffff:192.168.1.20", 40000), lan, true},
		{"own IPv6 address", tcpAddr("2001:db8::20", 40000), tcpAddr("2001:db8::20", 6270), true},
		{"own IPv6 link-local address", tcpAddr("fe80::1", 40000), tcpAddr("fe80::1", 6270), true},
		{"own IPv6 link-local address with zone", &net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 40000, Zone: "en0"}, tcpAddr("fe80::1", 6270), true},
		{"other IPv4 address", tcpAddr("192.168.1.30", 40000), lan, false},
		{"other IPv4 address, IPv4-mapped form", tcpAddr("::ffff:192.168.1.30", 40000), lan, false},
		{"other IPv6 address", tcpAddr("2001:db8::30", 40000), tcpAddr("2001:db8::20", 6270), false},
		{"no remote IP", &net.TCPAddr{Port: 40000}, lan, false},
		{"nil remote", nil, lan, false},
		{"nil local", tcpAddr("127.0.0.1", 40000), nil, false},
		{"unix socket", &net.UnixAddr{Name: "rhyolite.sock", Net: "unix"}, lan, false},
		{"other address type with own IP", textAddr("192.168.1.20:40000"), lan, true},
		{"other address type with IPv6 zone", textAddr("[fe80::1%en0]:40000"), tcpAddr("fe80::1", 6270), true},
		{"other address type without port", textAddr("192.168.1.20"), lan, false},
		{"unreadable remote", textAddr("garbage"), lan, false},
		{"unreadable local with loopback remote", tcpAddr("127.0.0.1", 40000), textAddr("garbage"), false},
	}
	for _, c := range cases {
		if got := localConn(c.remote, c.local); got != c.want {
			t.Errorf("%s: localConn = %v, want %v", c.name, got, c.want)
		}
	}
}

// fakeConn is a connection with fixed addresses that records Close. It carries
// no data, so Read and Write are never called.
type fakeConn struct {
	net.Conn
	remote, local net.Addr
	closed        bool
}

func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) LocalAddr() net.Addr  { return c.local }

func (c *fakeConn) Close() error {
	c.closed = true
	return nil
}

// acceptResult is one scripted outcome of fakeListener.Accept.
type acceptResult struct {
	conn net.Conn
	err  error
}

// fakeListener returns its scripted outcomes from Accept, then net.ErrClosed.
type fakeListener struct {
	results []acceptResult
	closed  bool
}

func (l *fakeListener) Accept() (net.Conn, error) {
	if len(l.results) == 0 {
		return nil, net.ErrClosed
	}
	r := l.results[0]
	l.results = l.results[1:]
	return r.conn, r.err
}

func (l *fakeListener) Close() error {
	l.closed = true
	return nil
}

func (l *fakeListener) Addr() net.Addr { return tcpAddr("192.168.1.20", 6270) }

// Connections from elsewhere are closed and skipped; the next local one is
// returned.
func TestLocalOnlySkipsRemoteConns(t *testing.T) {
	lan := tcpAddr("192.168.1.20", 6270)
	away := &fakeConn{remote: tcpAddr("10.0.0.9", 40001), local: lan}
	here := &fakeConn{remote: tcpAddr("127.0.0.1", 40002), local: lan}
	lg := &memLog{}
	ln := LocalOnly(&fakeListener{results: []acceptResult{{conn: away}, {conn: here}}}, lg)
	conn, err := ln.Accept()
	if err != nil || conn != here {
		t.Fatalf("Accept = %v, %v; want the local connection", conn, err)
	}
	if !away.closed || here.closed {
		t.Errorf("closed: remote %v, local %v; want true, false", away.closed, here.closed)
	}
	if got := countLines(lg, "event=refused ip=10.0.0.9"); got != 1 {
		t.Errorf("refused lines = %d:\n%s", got, lg.text())
	}
}

// Each refused IP is logged once, however many connections it makes.
func TestLocalOnlyLogsEachRefusedIPOnce(t *testing.T) {
	lan := tcpAddr("192.168.1.20", 6270)
	results := []acceptResult{
		{conn: &fakeConn{remote: tcpAddr("10.0.0.9", 40001), local: lan}},
		{conn: &fakeConn{remote: tcpAddr("10.0.0.9", 40002), local: lan}},
		{conn: &fakeConn{remote: tcpAddr("10.0.0.8", 40003), local: lan}},
		{conn: &fakeConn{remote: tcpAddr("127.0.0.1", 40004), local: lan}},
	}
	lg := &memLog{}
	ln := LocalOnly(&fakeListener{results: results}, lg)
	if _, err := ln.Accept(); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"10.0.0.9", "10.0.0.8"} {
		if got := countLines(lg, "event=refused ip="+ip); got != 1 {
			t.Errorf("refused lines for %s = %d:\n%s", ip, got, lg.text())
		}
	}
	if got := countLines(lg, "event=refused"); got != 2 {
		t.Errorf("refused lines = %d:\n%s", got, lg.text())
	}
}

// An error from the listener is returned as it is, also after a refused
// connection, which is still closed. A nil log is allowed.
func TestLocalOnlyReturnsListenerErrors(t *testing.T) {
	boom := errors.New("accept failed")
	away := &fakeConn{remote: tcpAddr("10.0.0.9", 40001), local: tcpAddr("192.168.1.20", 6270)}
	ln := LocalOnly(&fakeListener{results: []acceptResult{{conn: away}, {err: boom}}}, nil)
	conn, err := ln.Accept()
	if conn != nil || err != boom {
		t.Errorf("Accept = %v, %v; want nil, %v", conn, err, boom)
	}
	if !away.closed {
		t.Error("refused connection not closed")
	}
}

// Addr and Close are those of the wrapped listener.
func TestLocalOnlyDelegatesAddrAndClose(t *testing.T) {
	inner := &fakeListener{}
	ln := LocalOnly(inner, nil)
	if got := ln.Addr().String(); got != "192.168.1.20:6270" {
		t.Errorf("Addr = %s", got)
	}
	if err := ln.Close(); err != nil || !inner.closed {
		t.Errorf("Close: err %v, inner closed %v", err, inner.closed)
	}
}

// A dial from this machine over a real socket is accepted.
func TestLocalOnlyAcceptsLoopbackDial(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// If the dial were refused, Accept would wait for another connection; the
	// deadline turns that wait into an error.
	inner.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	ln := LocalOnly(inner, nil)
	defer ln.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}
