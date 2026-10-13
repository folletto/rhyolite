package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRollsOverOnMonthChange(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)}
	w, err := newWithClock(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	w.Log("september")
	c.t = time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC)
	w.Log("october")
	w.Close()

	if got := read(t, filepath.Join(dir, "rhyolite.202609.log")); !strings.Contains(got, "september") || strings.Contains(got, "october") {
		t.Errorf("archive = %q", got)
	}
	if got := read(t, filepath.Join(dir, "rhyolite.log")); !strings.Contains(got, "october") || strings.Contains(got, "september") {
		t.Errorf("current = %q", got)
	}
}

func TestAppendsAcrossRestartInSameMonth(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	w, _ := newWithClock(dir, c.now)
	w.Log("one")
	w.Close()
	w, _ = newWithClock(dir, c.now)
	w.Log("two")
	w.Close()
	got := read(t, filepath.Join(dir, "rhyolite.log"))
	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Errorf("log = %q", got)
	}
}

func TestStaleFileFromEarlierMonthIsArchivedAtStartup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rhyolite.log")
	os.WriteFile(p, []byte("old\n"), 0o600)
	old := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	os.Chtimes(p, old, old)
	c := &clock{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	w, err := newWithClock(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	w.Log("new")
	w.Close()
	if got := read(t, filepath.Join(dir, "rhyolite.202608.log")); got != "old\n" {
		t.Errorf("archive = %q", got)
	}
}

func TestExistingArchiveIsNotClobbered(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rhyolite.202609.log"), []byte("earlier\n"), 0o600)
	c := &clock{time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}
	w, _ := newWithClock(dir, c.now)
	w.Log("late")
	c.t = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	w.Log("next")
	w.Close()
	got := read(t, filepath.Join(dir, "rhyolite.202609.log"))
	if !strings.HasPrefix(got, "earlier\n") || !strings.Contains(got, "late") {
		t.Errorf("archive = %q", got)
	}
}

func TestEchoMatchesFile(t *testing.T) {
	dir := t.TempDir()
	c := &clock{time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	w, err := newWithClock(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	var echo strings.Builder
	w.SetEcho(&echo)
	w.Log("hello", "n", 1)
	w.Write([]byte("raw line\n"))
	w.Close()

	got := read(t, filepath.Join(dir, "rhyolite.log"))
	if got != echo.String() || !strings.Contains(got, "event=hello n=1\n") || !strings.Contains(got, "raw line\n") {
		t.Errorf("file = %q, echo = %q", got, echo.String())
	}
}

func TestFormat(t *testing.T) {
	ts := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		kv   []any
		want string
	}{
		{nil, "ts=2026-09-30T12:00:00Z event=e"},
		{[]any{"path", "a/b.md"}, "ts=2026-09-30T12:00:00Z event=e path=a/b.md"},
		{[]any{"path", "my notes/a b.md"}, `ts=2026-09-30T12:00:00Z event=e path="my notes/a b.md"`},
		{[]any{"v", ""}, `ts=2026-09-30T12:00:00Z event=e v=""`},
		{[]any{"v", `say "hi"`}, `ts=2026-09-30T12:00:00Z event=e v="say \"hi\""`},
		{[]any{"v", "a=b"}, `ts=2026-09-30T12:00:00Z event=e v="a=b"`},
		{[]any{"v", "a\nb"}, `ts=2026-09-30T12:00:00Z event=e v="a\nb"`},
		{[]any{"path", "caffè/日本.md"}, "ts=2026-09-30T12:00:00Z event=e path=caffè/日本.md"},
		{[]any{"status", 200, "dur_ms", 12*time.Millisecond + 600*time.Microsecond}, "ts=2026-09-30T12:00:00Z event=e status=200 dur_ms=12"},
	}
	for _, c := range cases {
		if got := Format(ts, "e", c.kv...); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.kv, got, c.want)
		}
	}
}

// parseLogfmt is a minimal logfmt parser used to prove lines round-trip.
func parseLogfmt(line string) (keys, vals []string, err error) {
	for i := 0; i < len(line); {
		eq := strings.IndexByte(line[i:], '=')
		if eq < 0 {
			return nil, nil, fmt.Errorf("no = in %q", line[i:])
		}
		k := line[i : i+eq]
		i += eq + 1
		var v string
		if i < len(line) && line[i] == '"' {
			// Find the end of the quoted string.
			j := i + 1
			for j < len(line) && line[j] != '"' {
				if line[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(line) {
				return nil, nil, fmt.Errorf("unterminated quote in %q", line)
			}
			v, err = strconv.Unquote(line[i : j+1])
			if err != nil {
				return nil, nil, err
			}
			i = j + 1
		} else {
			j := strings.IndexByte(line[i:], ' ')
			if j < 0 {
				j = len(line) - i
			}
			v = line[i : i+j]
			i += j
		}
		keys, vals = append(keys, k), append(vals, v)
		if i < len(line) {
			if line[i] != ' ' {
				return nil, nil, fmt.Errorf("expected space at %d in %q", i, line)
			}
			i++
		}
	}
	return keys, vals, nil
}

func TestFormatRoundTrips(t *testing.T) {
	ts := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	values := []string{"", "plain", "a b", "a=b", `q"q`, "tab\there", "line\nbreak", "日本/ü.md", `back\slash`, " lead", "trail ", "x=\"y z\""}
	for _, v := range values {
		line := Format(ts, "tool", "name", "read", "path", v, "result", "ok")
		if strings.ContainsAny(line, "\n\r") {
			t.Fatalf("line has newline: %q", line)
		}
		keys, vals, err := parseLogfmt(line)
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		wantK := []string{"ts", "event", "name", "path", "result"}
		wantV := []string{"2026-09-30T12:00:00Z", "tool", "read", v, "ok"}
		if fmt.Sprint(keys) != fmt.Sprint(wantK) || fmt.Sprint(vals) != fmt.Sprint(wantV) {
			t.Errorf("%q: parsed %v %q", v, keys, vals)
		}
	}
}
