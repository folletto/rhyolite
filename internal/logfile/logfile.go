// Package logfile writes rhyolite.log, rolling it over every month.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer appends to dir/rhyolite.log. When a write happens in a later month
// than the one the file was started in, the file is renamed to
// rhyolite.{yyyymm}.log (the month of its content) and a new one begins.
type Writer struct {
	mu    sync.Mutex
	dir   string
	now   func() time.Time
	f     *os.File
	month string // yyyymm of the open file
}

// New opens (or creates) the log in dir.
func New(dir string) (*Writer, error) { return newWithClock(dir, time.Now) }

func newWithClock(dir string, now func() time.Time) (*Writer, error) {
	w := &Writer{dir: dir, now: now}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) path() string { return filepath.Join(w.dir, "rhyolite.log") }

func (w *Writer) open() error {
	// The month of an existing file is the month of its last modification.
	month := w.now().Format("200601")
	if info, err := os.Stat(w.path()); err == nil {
		month = info.ModTime().Format("200601")
	}
	f, err := os.OpenFile(w.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.f, w.month = f, month
	return w.rotateIfNeeded()
}

func (w *Writer) rotateIfNeeded() error {
	cur := w.now().Format("200601")
	if cur == w.month {
		return nil
	}
	w.f.Close()
	old := filepath.Join(w.dir, fmt.Sprintf("rhyolite.%s.log", w.month))
	if _, err := os.Stat(old); err == nil {
		// Never clobber an archive: append the old content to it.
		if err := appendFile(old, w.path()); err != nil {
			return err
		}
		os.Remove(w.path())
	} else if err := os.Rename(w.path(), old); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.f, w.month = f, cur
	return nil
}

func appendFile(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// Printf writes one timestamped line.
func (w *Writer) Printf(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(); err != nil {
		return
	}
	fmt.Fprintf(w.f, "%s %s\n", w.now().Format("2006-01-02T15:04:05Z07:00"), fmt.Sprintf(format, args...))
}

// Write lets the Writer be used as an io.Writer (e.g. by net/http error logs).
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(); err != nil {
		return 0, err
	}
	return w.f.Write(p)
}

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
