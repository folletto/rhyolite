package logfile

import (
	"os"
	"path/filepath"
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
	w.Printf("september")
	c.t = time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC)
	w.Printf("october")
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
	w.Printf("one")
	w.Close()
	w, _ = newWithClock(dir, c.now)
	w.Printf("two")
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
	w.Printf("new")
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
	w.Printf("late")
	c.t = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	w.Printf("next")
	w.Close()
	got := read(t, filepath.Join(dir, "rhyolite.202609.log"))
	if !strings.HasPrefix(got, "earlier\n") || !strings.Contains(got, "late") {
		t.Errorf("archive = %q", got)
	}
}
