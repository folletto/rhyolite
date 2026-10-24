package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/folletto/rhyolite/internal/trash"
	"github.com/folletto/rhyolite/internal/vault"
)

func fromIP(h http.Handler, ip, token string) {
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	req.RemoteAddr = ip + ":5555"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func clientEnv(t *testing.T) (http.Handler, *memLog) {
	t.Helper()
	v, err := vault.Open(t.TempDir(), trash.Func(os.RemoveAll))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	lg := &memLog{}
	return New(Options{Vault: v, Token: testToken, Log: lg}), lg
}

func countLines(lg *memLog, prefix string) int {
	n := 0
	for _, l := range strings.Split(lg.text(), "\n") {
		if strings.Contains(l, prefix) {
			n++
		}
	}
	return n
}

func TestClientNewOncePerIP(t *testing.T) {
	h, lg := clientEnv(t)
	fromIP(h, "10.0.0.1", testToken)
	fromIP(h, "10.0.0.1", testToken)
	if got := countLines(lg, "event=client_new ip=10.0.0.1 auth=ok"); got != 1 {
		t.Errorf("client_new lines = %d:\n%s", got, lg.text())
	}
	fromIP(h, "10.0.0.2", testToken)
	if got := countLines(lg, "event=client_new"); got != 2 {
		t.Errorf("client_new lines = %d:\n%s", got, lg.text())
	}
	// client_new precedes the request's own http line.
	lines := strings.Split(lg.text(), "\n")
	if !strings.Contains(lines[0], "event=client_new") || !strings.Contains(lines[1], "event=http") {
		t.Errorf("order:\n%s", lg.text())
	}
}

func TestClientNewAuthFail(t *testing.T) {
	h, lg := clientEnv(t)
	fromIP(h, "10.0.0.9", "wrong")
	lines := strings.Split(lg.text(), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "event=client_new ip=10.0.0.9 auth=fail") || !strings.Contains(lines[1], "event=auth_fail") {
		t.Errorf("log:\n%s", lg.text())
	}
	fromIP(h, "10.0.0.9", testToken)
	if got := countLines(lg, "event=client_new"); got != 1 {
		t.Errorf("client_new lines = %d", got)
	}
}

func TestClientTrackerCap(t *testing.T) {
	var c clientTracker
	for i := 0; i < maxClients; i++ {
		if !c.isNew(fmt.Sprint("ip", i)) {
			t.Fatalf("ip%d not new", i)
		}
	}
	if c.isNew("overflow") || c.isNew("overflow") {
		t.Error("IPs past the cap must not be announced")
	}
	if c.isNew("ip0") {
		t.Error("known IP reported new")
	}
}

// logLine returns the first line of lg that contains all of subs, or "".
func logLine(lg *memLog, subs ...string) string {
	for _, l := range strings.Split(lg.text(), "\n") {
		match := true
		for _, s := range subs {
			match = match && strings.Contains(l, s)
		}
		if match {
			return l
		}
	}
	return ""
}

// spoofRT sends a forged client IP header with every request.
type spoofRT struct{ next http.RoundTripper }

func (s spoofRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(clientIPHeader, "6.6.6.6")
	return s.next.RoundTrip(r)
}

func TestClientInitLogged(t *testing.T) {
	e := newEnv(t)
	e.connect()
	if got := countLines(e.log, "event=client_init ip=127.0.0.1 session="); got != 1 {
		t.Fatalf("client_init lines = %d:\n%s", got, e.log.text())
	}
	initLine := logLine(e.log, "event=client_init")
	if !strings.Contains(initLine, " name=test version=1") {
		t.Fatalf("client_init line: %s", initLine)
	}
	// The session id is the token after "session=", up to the next space.
	_, rest, _ := strings.Cut(initLine, " session=")
	sid, _, _ := strings.Cut(rest, " ")
	if sid == "" {
		t.Fatalf("client_init line has no session id: %s", initLine)
	}
	// An http line is written when its handler returns, which can be just after
	// the client gets its response, so allow a moment for it.
	deadline := time.Now().Add(5 * time.Second)
	for logLine(e.log, "event=http", " session="+sid) == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no http line with session=%s:\n%s", sid, e.log.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClientInitIPCannotBeSpoofed(t *testing.T) {
	e := newEnv(t)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: e.ts.URL + "/mcp", HTTPClient: &http.Client{Transport: spoofRT{authRT{testToken}}}}, nil)
	if err != nil {
		t.Fatal("connect:", err)
	}
	t.Cleanup(func() { cs.Close() })
	if got := countLines(e.log, "event=client_init ip=127.0.0.1 session="); got != 1 {
		t.Fatalf("client_init lines = %d:\n%s", got, e.log.text())
	}
	if strings.Contains(e.log.text(), "6.6.6.6") {
		t.Errorf("forged IP logged:\n%s", e.log.text())
	}
}
