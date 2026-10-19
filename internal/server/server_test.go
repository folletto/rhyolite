package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/folletto/rhyolite/internal/logfile"
	"github.com/folletto/rhyolite/internal/trash"
	"github.com/folletto/rhyolite/internal/vault"
)

const testToken = "test-token-123"

type memLog struct {
	mu    sync.Mutex
	lines []string
}

func (m *memLog) Log(event string, kv ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lines = append(m.lines, logfile.Format(time.Unix(0, 0).UTC(), event, kv...))
}

func (m *memLog) text() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.lines, "\n")
}

type env struct {
	t   *testing.T
	dir string
	ts  *httptest.Server
	log *memLog
}

func newEnv(t *testing.T, localNames ...string) *env {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	v, err := vault.Open(dir, trash.Func(func(p string) error { return os.RemoveAll(p) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	lg := &memLog{}
	ts := httptest.NewServer(New(Options{Vault: v, Token: testToken, Log: lg, Version: "test", LocalNames: localNames}))
	t.Cleanup(ts.Close)
	return &env{t: t, dir: dir, ts: ts, log: lg}
}

type authRT struct{ token string }

func (a authRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if a.token != "" {
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) connect() *mcp.ClientSession {
	e.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: e.ts.URL + "/mcp", HTTPClient: &http.Client{Transport: authRT{testToken}}}, nil)
	if err != nil {
		e.t.Fatal("connect:", err)
	}
	e.t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool; on a tool error it returns the error text.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		var msg string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg += tc.Text
			}
		}
		return nil, msg
	}
	out := map[string]any{}
	b, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(b, &out)
	return out, ""
}

func ok(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	out, errText := call(t, cs, name, args)
	if errText != "" {
		t.Fatalf("%s failed: %s", name, errText)
	}
	return out
}

func fails(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, contains string) {
	t.Helper()
	_, errText := call(t, cs, name, args)
	if errText == "" || !strings.Contains(errText, contains) {
		t.Fatalf("%s: expected error containing %q, got %q", name, contains, errText)
	}
}

func TestToolList(t *testing.T) {
	e := newEnv(t)
	cs := e.connect()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "list_recursive", "file_read", "file_meta_read", "file_meta_write", "file_write", "file_append", "file_patch", "file_move", "file_delete", "folder_create", "folder_move", "folder_delete", "search"}
	got := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
	}
	for _, n := range want {
		tl := got[n]
		if tl == nil {
			t.Errorf("missing tool %s", n)
			continue
		}
		if tl.Description == "" || tl.Annotations == nil {
			t.Errorf("tool %s lacks description/annotations", n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d tools, want %d (folder_rename must not exist)", len(got), len(want))
	}
	for _, n := range []string{"list", "list_recursive", "file_read", "file_meta_read", "search"} {
		if !got[n].Annotations.ReadOnlyHint {
			t.Errorf("%s should be read-only", n)
		}
	}
	if d := got["file_delete"].Annotations.DestructiveHint; d == nil || !*d {
		t.Error("file_delete should be destructive")
	}
}

func TestEveryToolOverHTTP(t *testing.T) {
	e := newEnv(t)
	cs := e.connect()

	// folders
	ok(t, cs, "folder_create", map[string]any{"path": "projects/alpha"})
	// write + read
	w := ok(t, cs, "file_write", map[string]any{"path": "projects/alpha/plan.md", "content": "---\ntitle: Plan\n---\n# Plan\nstep one\n"})
	if w["created"] != true || w["hash"] == "" {
		t.Fatalf("write: %v", w)
	}
	fails(t, cs, "file_write", map[string]any{"path": "projects/alpha/plan.md", "content": "x"}, "already exists")
	r := ok(t, cs, "file_read", map[string]any{"path": "projects/alpha/plan.md"})
	if r["frontmatter"].(map[string]any)["title"] != "Plan" || r["total_lines"] != float64(5) || r["hash"] != w["hash"] {
		t.Fatalf("read: %v", r)
	}
	// meta
	m := ok(t, cs, "file_meta_read", map[string]any{"path": "projects/alpha/plan.md"})
	if m["frontmatter"].(map[string]any)["title"] != "Plan" {
		t.Fatalf("meta read: %v", m)
	}
	m = ok(t, cs, "file_meta_write", map[string]any{"path": "projects/alpha/plan.md", "fields": map[string]any{"status": "draft", "title": "", "n": 2}})
	fm := m["frontmatter"].(map[string]any)
	if fm["status"] != "draft" || fm["n"] != float64(2) || fm["title"] != nil {
		t.Fatalf("meta write: %v", m)
	}
	// patch + append
	ok(t, cs, "file_patch", map[string]any{"path": "projects/alpha/plan.md", "old_string": "step one", "new_string": "step 1"})
	fails(t, cs, "file_patch", map[string]any{"path": "projects/alpha/plan.md", "old_string": "nope", "new_string": "x"}, "not found")
	ok(t, cs, "file_append", map[string]any{"path": "projects/alpha/plan.md", "content": "step 2\n"})
	if got := disk(t, e.dir, "projects/alpha/plan.md"); got != "---\nn: 2\nstatus: draft\n---\n# Plan\nstep 1\nstep 2\n" {
		t.Fatalf("file = %q", got)
	}
	// list, list_recursive
	l := ok(t, cs, "list", map[string]any{})
	if items := l["items"].([]any); len(items) != 1 || items[0].(map[string]any)["name"] != "projects" {
		t.Fatalf("list: %v", l)
	}
	lr := ok(t, cs, "list_recursive", map[string]any{"type": "files"})
	if items := lr["items"].([]any); len(items) != 1 || items[0].(map[string]any)["path"] != "projects/alpha/plan.md" || lr["has_more"] != false {
		t.Fatalf("list_recursive: %v", lr)
	}
	// search
	s := ok(t, cs, "search", map[string]any{"query": "STEP", "mode": "content"})
	hit := s["results"].([]any)[0].(map[string]any)
	if hit["path"] != "projects/alpha/plan.md" || hit["frontmatter"].(map[string]any)["status"] != "draft" {
		t.Fatalf("search: %v", s)
	}
	// move + rename
	ok(t, cs, "file_move", map[string]any{"path": "projects/alpha/plan.md", "destination": "archive/2026/plan-final.md"})
	ok(t, cs, "folder_move", map[string]any{"path": "archive", "destination": "old-archive"})
	if disk(t, e.dir, "old-archive/2026/plan-final.md") == "" {
		t.Fatal("moves failed")
	}
	// delete
	d := ok(t, cs, "file_delete", map[string]any{"path": "old-archive/2026/plan-final.md"})
	if d["trash"] != "system" {
		t.Fatalf("delete: %v", d)
	}
	fails(t, cs, "folder_delete", map[string]any{"path": "projects"}, "not empty")
	ok(t, cs, "folder_delete", map[string]any{"path": "projects/alpha"})
	ok(t, cs, "folder_delete", map[string]any{"path": "projects"})
	ok(t, cs, "folder_delete", map[string]any{"path": "old-archive/2026"})
}

func disk(t *testing.T, dir, rel string) string {
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestErrorsReachTheModelWithoutHostPaths(t *testing.T) {
	e := newEnv(t)
	cs := e.connect()
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"file_read", map[string]any{"path": "missing.md"}, "not found"},
		{"file_read", map[string]any{"path": "../../etc/passwd"}, "invalid path"},
		{"file_read", map[string]any{"path": "/etc/passwd"}, "relative"},
		{"file_read", map[string]any{"path": "rhyolite.json"}, ".md"},
		{"list", map[string]any{"path": "nope"}, "not found"},
		{"list", map[string]any{"type": "bogus"}, "type must be"},
		{"search", map[string]any{"query": ""}, "must not be empty"},
		{"file_write", map[string]any{"path": "a.txt", "content": "x"}, ".md"},
	} {
		_, msg := call(t, cs, c.tool, c.args)
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s %v: error %q lacks %q", c.tool, c.args, msg, c.want)
		}
		if strings.Contains(msg, e.dir) || strings.Contains(msg, "/private/") {
			t.Errorf("%s leaks host path: %q", c.tool, msg)
		}
	}
}

func TestStaleNoticeAcrossTwoSessions(t *testing.T) {
	e := newEnv(t)
	a, b := e.connect(), e.connect()
	ok(t, a, "file_write", map[string]any{"path": "n.md", "content": "alpha\n\nbeta\n"})
	ok(t, a, "file_read", map[string]any{"path": "n.md"})
	ok(t, b, "file_patch", map[string]any{"path": "n.md", "old_string": "alpha", "new_string": "ALPHA"})
	// a's view is stale: the edit still applies, with a notice
	r := ok(t, a, "file_patch", map[string]any{"path": "n.md", "old_string": "beta", "new_string": "BETA"})
	if r["notice"] == nil || r["notice"] == "" {
		t.Fatalf("no notice: %v", r)
	}
	if disk(t, e.dir, "n.md") != "ALPHA\n\nBETA\n" {
		t.Fatalf("file = %q", disk(t, e.dir, "n.md"))
	}
	// whole-file overwrite from the stale session is refused
	ok(t, b, "file_patch", map[string]any{"path": "n.md", "old_string": "ALPHA", "new_string": "A2"})
	fails(t, a, "file_write", map[string]any{"path": "n.md", "content": "mine", "overwrite": true}, "modified since")
	// expected_hash is strict
	h := ok(t, b, "file_meta_read", map[string]any{"path": "n.md"})["hash"]
	ok(t, a, "file_append", map[string]any{"path": "n.md", "content": "more\n"})
	fails(t, b, "file_patch", map[string]any{"path": "n.md", "old_string": "A2", "new_string": "x", "expected_hash": h}, "modified since")
}

func TestConcurrentClientsDoNotCorruptFiles(t *testing.T) {
	e := newEnv(t)
	setup := e.connect()
	ok(t, setup, "file_write", map[string]any{"path": "log.md", "content": "start\n"})
	var wg sync.WaitGroup
	const clients, per = 5, 8
	for i := 0; i < clients; i++ {
		cs := e.connect()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				if _, msg := call(t, cs, "file_append", map[string]any{"path": "log.md", "content": "entry\n"}); msg != "" {
					t.Error(msg)
				}
			}
		}()
	}
	wg.Wait()
	if n := strings.Count(disk(t, e.dir, "log.md"), "entry\n"); n != clients*per {
		t.Fatalf("%d entries, want %d", n, clients*per)
	}
}

// initializeBody is an MCP initialize request.
const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`

func rawRequest(t *testing.T, e *env, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(initializeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAuthentication(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"no header", nil, 401},
		{"wrong token", map[string]string{"Authorization": "Bearer nope"}, 401},
		{"empty token", map[string]string{"Authorization": "Bearer "}, 401},
		{"wrong scheme", map[string]string{"Authorization": "Basic " + testToken}, 401},
		{"token without scheme", map[string]string{"Authorization": testToken}, 401},
		{"prefix of token", map[string]string{"Authorization": "Bearer test-token-12"}, 401},
		{"token plus suffix", map[string]string{"Authorization": "Bearer " + testToken + "x"}, 401},
		{"correct", map[string]string{"Authorization": "Bearer " + testToken}, 200},
	}
	for _, c := range cases {
		if got := rawRequest(t, e, c.hdr).StatusCode; got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
	if !strings.Contains(e.log.text(), "event=auth_fail method=") {
		t.Error("auth failures not logged")
	}
	if strings.Contains(e.log.text(), testToken) {
		t.Error("token leaked into log")
	}
	resp := rawRequest(t, e, nil)
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate")
	}
	// other paths need auth too and don't exist
	req, _ := http.NewRequest("GET", e.ts.URL+"/", nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 401 {
		t.Errorf("/ without token: %d", r.StatusCode)
	}
}

func TestOriginGuard(t *testing.T) {
	e := newEnv(t)
	auth := "Bearer " + testToken
	cases := map[string]int{
		"https://evil.example.com": 403,
		"http://attacker.com:6270": 403,
		"null":                     403,
		"http://localhost:3000":    200,
		"http://127.0.0.1:6270":    200,
		"http://192.168.1.20:6270": 200,
		"http://10.0.0.5":          200,
		"http://macbook.local":     200,
		"http://8.8.8.8":           403,
	}
	for origin, want := range cases {
		if got := rawRequest(t, e, map[string]string{"Authorization": auth, "Origin": origin}).StatusCode; got != want {
			t.Errorf("Origin %q: %d, want %d", origin, got, want)
		}
	}
}

func TestHostGuard(t *testing.T) {
	loopback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 6270}
	loopback6 := &net.TCPAddr{IP: net.IPv6loopback, Port: 6270}
	lan := &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 6270}
	cases := []struct {
		name  string
		names []string
		local net.Addr // nil: no local address in the request context
		host  string
		want  int
	}{
		{"localhost", nil, loopback, "localhost:6270", 200},
		{"localhost in any case", nil, loopback, "LocalHost:6270", 200},
		{"IPv4 loopback", nil, loopback, "127.0.0.1:6270", 200},
		{"IPv6 loopback", nil, loopback6, "[::1]:6270", 200},
		{"IPv4 loopback without port", nil, loopback, "127.0.0.1", 200},
		{"IPv6 loopback without port", nil, loopback6, "[::1]", 200},
		{"local name", []string{"wombat.local"}, loopback, "wombat.local:6270", 200},
		{"local name in any case", []string{"wombat.local"}, loopback, "WOMBAT.local:6270", 200},
		{"local name with trailing dot", []string{"wombat.local"}, loopback, "wombat.local.:6270", 200},
		{"configured name with trailing dot", []string{"wombat.local."}, loopback, "wombat.local:6270", 200},
		{"advertised IP", []string{"192.168.1.20"}, loopback, "192.168.1.20:6270", 200},
		{"local name not configured", nil, loopback, "wombat.local:6270", 403},
		{"other name", []string{"wombat.local"}, loopback, "evil.example:6270", 403},
		{"name containing a configured one", []string{"wombat.local"}, loopback, "wombat.local.evil.example:6270", 403},
		{"empty configured name ignored", []string{""}, loopback, "evil.example:6270", 403},
		{"empty Host", []string{""}, loopback, "", 403},
		{"LAN connection passes", nil, lan, "evil.example:6270", 200},
		{"no local address passes", nil, nil, "evil.example:6270", 200},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Host = c.host
		if c.local != nil {
			req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, c.local))
		}
		rec := httptest.NewRecorder()
		hostGuard(c.names, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s (Host %q): status %d, want %d", c.name, c.host, rec.Code, c.want)
		}
		if c.want == 403 && !strings.Contains(rec.Body.String(), "Forbidden: invalid Host header") {
			t.Errorf("%s: body %q", c.name, rec.Body.String())
		}
	}
}

// With all set (the listener is wrapped with LocalOnly), the Host check also
// covers a connection on a LAN address: a foreign Host is refused there, while
// the connection's own address and the configured names pass.
func TestHostGuardAllConnections(t *testing.T) {
	lan := &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 6270}
	lan6 := &net.TCPAddr{IP: net.ParseIP("2001:db8::20"), Port: 6270}
	cases := []struct {
		name  string
		local *net.TCPAddr
		names []string
		host  string
		want  int
	}{
		{"local name", lan, []string{"wombat.local"}, "wombat.local:6270", 200},
		{"own address in names", lan, []string{"192.168.1.20"}, "192.168.1.20:6270", 200},
		{"own address", lan, nil, "192.168.1.20:6270", 200},
		{"other address", lan, nil, "192.168.1.21:6270", 403},
		{"own IPv6 address", lan6, nil, "[2001:db8::20]:6270", 200},
		{"own IPv6 address without port", lan6, nil, "[2001:db8::20]", 200},
		{"other IPv6 address", lan6, nil, "[2001:db8::21]:6270", 403},
		{"localhost", lan, nil, "localhost:6270", 200},
		{"IPv4 loopback", lan, nil, "127.0.0.1:6270", 200},
		{"foreign name", lan, []string{"wombat.local"}, "evil.example:6270", 403},
		{"local name not configured", lan, nil, "wombat.local:6270", 403},
		{"no local address", nil, nil, "192.168.1.20:6270", 403},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Host = c.host
		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, c.local))
		rec := httptest.NewRecorder()
		hostGuard(c.names, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s (Host %q): status %d, want %d", c.name, c.host, rec.Code, c.want)
		}
		if c.want == 403 && !strings.Contains(rec.Body.String(), "Forbidden: invalid Host header") {
			t.Errorf("%s: body %q", c.name, rec.Body.String())
		}
	}
}

// The SDK's own Host check is off, so the machine's name has to come from
// LocalNames: over real loopback HTTP that name works and other names do not.
func TestHostGuardOverHTTP(t *testing.T) {
	e := newEnv(t, "wombat.local")
	port := e.ts.Listener.Addr().(*net.TCPAddr).Port
	for _, c := range []struct {
		host string
		want int
	}{
		{"wombat.local", 200},
		{"localhost", 200},
		{"evil.example", 403},
	} {
		req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(initializeBody))
		req.Host = fmt.Sprintf("%s:%d", c.host, port)
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("Host %q: status %d, want %d", req.Host, resp.StatusCode, c.want)
		}
	}
}

func TestBodyLimit(t *testing.T) {
	e := newEnv(t)
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"x","params":{"pad":"` + strings.Repeat("a", 6<<20) + `"}}`)
	req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("oversized body accepted: %d", resp.StatusCode)
		}
	}
}

func TestToolCallsAreLoggedWithoutContent(t *testing.T) {
	e := newEnv(t)
	cs := e.connect()
	ok(t, cs, "file_write", map[string]any{"path": "secret.md", "content": "TOP-SECRET-CONTENT"})
	call(t, cs, "file_read", map[string]any{"path": "nope.md"})
	log := e.log.text()
	if !strings.Contains(log, "event=tool name=file_write path=secret.md result=ok dur_ms=") || !strings.Contains(log, "event=tool name=file_read path=nope.md result=error dur_ms=") || !strings.Contains(log, " error=") {
		t.Errorf("log:\n%s", log)
	}
	if strings.Contains(log, "TOP-SECRET-CONTENT") || strings.Contains(log, testToken) {
		t.Error("log leaks content or token")
	}
}
