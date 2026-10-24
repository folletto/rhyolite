package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Logger is what the server needs from a log: rhyolite.log in production.
// kv alternates string keys and values.
type Logger interface {
	Log(event string, kv ...any)
}

type nopLogger struct{}

func (nopLogger) Log(string, ...any) {}

// bearerAuth rejects requests without the right "Authorization: Bearer" token.
func bearerAuth(token string, log Logger, clients *clientTracker, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		valid := ok && subtle.ConstantTimeCompare(sum[:], want[:]) == 1
		if clients.isNew(clientIP(r)) {
			auth := "fail"
			if valid {
				auth = "ok"
			}
			log.Log("client_new", "ip", clientIP(r), "auth", auth)
		}
		if !valid {
			log.Log("auth_fail", "method", r.Method, "path", r.URL.Path, "ip", clientIP(r))
			w.Header().Set("WWW-Authenticate", `Bearer realm="rhyolite"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxClients bounds the set of remembered client IPs so scanners cannot grow
// it without limit; once full, no further IPs are recorded or announced.
const maxClients = 10000

// clientTracker remembers the client IPs seen since the server started (it
// is in memory only, so a restart announces every client again).
type clientTracker struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// isNew reports whether ip is seen for the first time, and records it.
func (c *clientTracker) isNew(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seen[ip]; ok || len(c.seen) >= maxClients {
		return false
	}
	if c.seen == nil {
		c.seen = map[string]struct{}{}
	}
	c.seen[ip] = struct{}{}
	return true
}

// hostGuard is the DNS-rebinding protection, in place of the MCP SDK's check.
// It checks the Host of connections that arrive over loopback, and of all
// connections when all is set (the LocalOnly listener admits only this
// machine's connections, see Options.LocalOnly). Other connections pass
// unchanged. The SDK accepts only localhost and loopback IPs as Host, which
// also refuses the machine's own name. On a Mac, "wombat.local" resolves to
// loopback, so a local client sends that name as Host: the names given (the
// machine's own names) are accepted too. So is the IP address the connection
// arrived on, so a LAN address works from this machine as well. A DNS-rebinding
// page sends its own domain name as Host, so it still gets 403.
func hostGuard(names []string, all bool, next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, n := range names {
		if h := hostKey(n); h != "" {
			allowed[h] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, _ := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
		if all || (local != nil && local.IP.IsLoopback()) {
			h := hostKey(r.Host)
			if !allowed[h] && !loopbackHost(h) && !localIPHost(h, local) {
				http.Error(w, fmt.Sprintf("Forbidden: invalid Host header %q", r.Host), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// hostKey returns the host of a Host header value or of a configured name, in
// lower case, without the port, IPv6 brackets or a trailing dot.
func hostKey(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
}

// loopbackHost reports whether a hostKey is localhost or a loopback IP.
func loopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// localIPHost reports whether a hostKey is the IP of local, the local address of
// a connection.
func localIPHost(h string, local *net.TCPAddr) bool {
	ip := net.ParseIP(h)
	return ip != nil && local != nil && ip.Equal(local.IP)
}

// originGuard blocks browser requests from web pages: a request carrying an
// Origin header is only allowed from loopback, private-network IPs, or local
// hostnames. Non-browser MCP clients send no Origin and pass. This is the
// DNS-rebinding protection the MCP transport spec asks for.
func originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && !localOrigin(o) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// clientIPHeader carries the connection's IP from requestLog to the MCP layer, which only sees headers.
// requestLog always overwrites it, so a client cannot set it.
const clientIPHeader = "X-Rhyolite-Client-Ip"

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }

// Flush keeps streaming (SSE) responses working through the wrapper.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// requestLog writes one line per HTTP request. The line carries the MCP session id when the request belongs to one.
func requestLog(log Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		r.Header.Set(clientIPHeader, clientIP(r))
		next.ServeHTTP(sw, r)
		// The server sets the session id on the response to initialize; later requests carry it in the request.
		session := sw.Header().Get("Mcp-Session-Id")
		if session == "" {
			session = r.Header.Get("Mcp-Session-Id")
		}
		kv := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "dur_ms", time.Since(start), "ip", clientIP(r)}
		if session != "" {
			kv = append(kv, "session", session)
		}
		log.Log("http", kv...)
	})
}

// limitBody caps request bodies.
func limitBody(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}
