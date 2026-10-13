package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
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
func bearerAuth(token string, log Logger, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			log.Log("auth_fail", "method", r.Method, "path", r.URL.Path, "ip", clientIP(r))
			w.Header().Set("WWW-Authenticate", `Bearer realm="rhyolite"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// requestLog writes one line per HTTP request.
func requestLog(log Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		log.Log("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur_ms", time.Since(start), "ip", clientIP(r))
	})
}

// limitBody caps request bodies.
func limitBody(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}
