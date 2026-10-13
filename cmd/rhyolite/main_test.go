package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "rhyolite ") {
		t.Errorf("output %q", out.String())
	}
}

func TestArgumentErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if err := run(context.Background(), []string{"a", "b"}, &out, &errb); err == nil {
		t.Error("two folders accepted")
	}
	if err := run(context.Background(), []string{filepath.Join(t.TempDir(), "missing")}, &out, &errb); err == nil {
		t.Error("missing folder accepted")
	}
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	if err := run(context.Background(), []string{f}, &out, &errb); err == nil {
		t.Error("file accepted as folder")
	}
	if out.Len() != 0 {
		t.Errorf("printed before failing: %q", out.String())
	}
}

func start(t *testing.T, args ...string) (out *syncBuf, stop func() error) {
	t.Helper()
	out = &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, args, out, out) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "Press Ctrl+C") {
		select {
		case err := <-done:
			t.Fatalf("exited early: %v\n%s", err, out.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("no banner:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	return out, func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("did not shut down")
			return nil
		}
	}
}

// extractConfigs pulls the two JSON blocks out of the banner.
func extractConfigs(t *testing.T, banner string) (remote, native map[string]any) {
	t.Helper()
	re := regexp.MustCompile(`(?s)\{\n.*?\n\}`)
	blocks := re.FindAllString(banner, -1)
	if len(blocks) != 2 {
		t.Fatalf("found %d JSON blocks in:\n%s", len(blocks), banner)
	}
	if err := json.Unmarshal([]byte(blocks[0]), &remote); err != nil {
		t.Fatalf("mcp-remote block is not JSON: %v\n%s", err, blocks[0])
	}
	if err := json.Unmarshal([]byte(blocks[1]), &native); err != nil {
		t.Fatalf("native block is not JSON: %v\n%s", err, blocks[1])
	}
	return
}

func TestStartupCreatesConfigServesAndStops(t *testing.T) {
	dir := t.TempDir()
	out, stop := start(t, "--port", "0", dir)

	if !strings.HasPrefix(out.String(), "▴ Creating rhyolite.json\n") {
		t.Errorf("first line: %q", strings.SplitN(out.String(), "\n", 2)[0])
	}
	var cfg struct{ Token string }
	b, err := os.ReadFile(filepath.Join(dir, "rhyolite.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &cfg)
	if cfg.Token == "" {
		t.Fatal("no token written")
	}

	remote, native := extractConfigs(t, out.String())
	rs := remote["mcpServers"].(map[string]any)["rhyolite"].(map[string]any)
	if rs["command"] != "npx" {
		t.Errorf("remote: %v", rs)
	}
	args := rs["args"].([]any)
	url := args[2].(string)
	if args[1] != "mcp-remote" || !strings.HasPrefix(url, "http://") || !strings.HasSuffix(url, "/mcp") || args[3] != "--allow-http" || args[5] != "Authorization:${AUTH_HEADER}" {
		t.Errorf("remote args: %v", args)
	}
	if rs["env"].(map[string]any)["AUTH_HEADER"] != "Bearer "+cfg.Token {
		t.Errorf("remote token: %v", rs["env"])
	}
	ns := native["mcpServers"].(map[string]any)["rhyolite"].(map[string]any)
	if ns["url"] != url || ns["headers"].(map[string]any)["Authorization"] != "Bearer "+cfg.Token {
		t.Errorf("native: %v", ns)
	}

	// The printed configuration really works: connect with it.
	c := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   strings.Replace(url, hostOf(url), "127.0.0.1", 1),
		HTTPClient: &http.Client{Transport: bearer(cfg.Token)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "file_write", Arguments: map[string]any{"path": "hello.md", "content": "hi\n"}}); err != nil {
		t.Fatal(err)
	}
	cs.Close()
	if got, _ := os.ReadFile(filepath.Join(dir, "hello.md")); string(got) != "hi\n" {
		t.Errorf("file = %q", got)
	}
	if err := stop(); err != nil {
		t.Errorf("shutdown: %v", err)
	}
	logb, _ := os.ReadFile(filepath.Join(dir, "rhyolite.log"))
	if !strings.Contains(string(logb), "event=start version=") || !strings.Contains(string(logb), "event=tool name=file_write path=hello.md result=ok") || strings.Contains(string(logb), cfg.Token) {
		t.Errorf("log:\n%s", logb)
	}
	// Every log line is echoed to the terminal after the banner, unchanged.
	term := out.String()
	if !strings.Contains(term, "Press Ctrl+C") {
		t.Fatalf("no banner:\n%s", term)
	}
	echoed := term[strings.Index(term, "Press Ctrl+C"):]
	for _, line := range strings.Split(strings.TrimSpace(string(logb)), "\n") {
		if !strings.Contains(echoed, line+"\n") {
			t.Errorf("log line not on terminal: %q", line)
		}
	}
}

func hostOf(url string) string {
	u := strings.TrimPrefix(url, "http://")
	return u[:strings.LastIndex(u, ":")]
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

func TestRestartReadsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	_, stop := start(t, "--port", "0", dir)
	first, _ := os.ReadFile(filepath.Join(dir, "rhyolite.json"))
	stop()

	out, stop := start(t, "--port", "0", dir)
	defer stop()
	if !strings.HasPrefix(out.String(), "▴ Reading rhyolite.json\n") {
		t.Errorf("first line: %q", strings.SplitN(out.String(), "\n", 2)[0])
	}
	second, _ := os.ReadFile(filepath.Join(dir, "rhyolite.json"))
	if string(first) != string(second) {
		t.Error("config rewritten on restart")
	}
}

func TestEmptyTokenIsReplacedOnStart(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rhyolite.json"), []byte(`{"version":1,"token":"","host":"127.0.0.1","port":6270}`), 0o600)
	out, stop := start(t, "--port", "0", dir)
	defer stop()
	var cfg struct {
		Token string
		Host  string
	}
	b, _ := os.ReadFile(filepath.Join(dir, "rhyolite.json"))
	json.Unmarshal(b, &cfg)
	if cfg.Token == "" || cfg.Host != "127.0.0.1" {
		t.Fatalf("config: %s", b)
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:") || !strings.Contains(out.String(), cfg.Token) {
		t.Errorf("banner does not use the configured host and new token:\n%s", out.String())
	}
}

func TestDefaultsToCurrentFolder(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	_, stop := start(t, "--port", "0")
	defer stop()
	if _, err := os.Stat(filepath.Join(dir, "rhyolite.json")); err != nil {
		t.Fatal("config not created in the current folder")
	}
}

func TestPortInUseIsAnError(t *testing.T) {
	dir := t.TempDir()
	out, stop := start(t, "--port", "0", dir)
	defer stop()
	m := regexp.MustCompile(`:(\d+)/mcp`).FindStringSubmatch(out.String())
	var o2, e2 bytes.Buffer
	if err := run(context.Background(), []string{"--port", m[1], t.TempDir()}, &o2, &e2); err == nil {
		t.Error("second server on the same port started")
	}
}

func TestVerbosityLevels(t *testing.T) {
	for _, c := range []struct {
		level          string
		banner, events bool
	}{{"none", false, false}, {"basic", true, false}, {"verbose", true, true}} {
		t.Run(c.level, func(t *testing.T) {
			dir := t.TempDir()
			out := &syncBuf{}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- run(ctx, []string{"--port", "0", "--verbosity", c.level, dir}, out, out) }()
			logPath := filepath.Join(dir, "rhyolite.log")
			deadline := time.Now().Add(5 * time.Second)
			for {
				if b, _ := os.ReadFile(logPath); strings.Contains(string(b), "event=start") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if c.level == "none" && got != "" {
				t.Errorf("stdout not empty: %q", got)
			}
			if has := strings.Contains(got, "Press Ctrl+C"); has != c.banner {
				t.Errorf("banner present = %v", has)
			}
			if has := strings.Contains(got, "event="); has != c.events {
				t.Errorf("log lines present = %v:\n%s", has, got)
			}
			if b, _ := os.ReadFile(logPath); !strings.Contains(string(b), "event=stop") {
				t.Errorf("log file incomplete:\n%s", b)
			}
		})
	}
}

func TestInvalidVerbosity(t *testing.T) {
	err := run(context.Background(), []string{"--verbosity", "loud", t.TempDir()}, &syncBuf{}, &syncBuf{})
	if err == nil || !strings.Contains(err.Error(), "none, basic or verbose") {
		t.Errorf("err = %v", err)
	}
}
