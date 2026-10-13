package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
