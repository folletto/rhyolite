// Package logfile writes rhyolite.log, rolling it over every month.
package logfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	month string          // yyyymm of the open file
	echo  io.Writer       // optional copy of every line, e.g. the terminal
	bold  map[string]bool // events printed in bold on the echo, when it is a terminal
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

// SetEcho makes the Writer also write every line to out, identical to what
// goes to the file. Pass nil to stop echoing. When out is a terminal, lines of
// the events named in highlight are wrapped in bold on the echo only; the file
// and non-terminal echoes never contain escape codes.
func (w *Writer) SetEcho(out io.Writer, highlight ...string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.echo = out
	w.bold = nil
	if isTerminal(out) {
		w.bold = map[string]bool{}
		for _, e := range highlight {
			w.bold[e] = true
		}
	}
}

func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Log writes one logfmt line: ts, event, then the alternating key/value pairs
// in kv. See Format.
func (w *Writer) Log(event string, kv ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(); err != nil {
		return
	}
	line := Format(w.now(), event, kv...) + "\n"
	io.WriteString(w.f, line)
	if w.echo != nil {
		if w.bold[event] {
			line = "\x1b[1m" + strings.TrimSuffix(line, "\n") + "\x1b[0m\n"
		}
		io.WriteString(w.echo, line)
	}
}

// Write lets the Writer be used as an io.Writer (e.g. by net/http error logs).
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(); err != nil {
		return 0, err
	}
	n, err := w.f.Write(p)
	if w.echo != nil {
		w.echo.Write(p[:n])
	}
	return n, err
}

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// Format renders one logfmt line (without the newline):
// ts=<RFC3339> event=<name> key=value ... with keys in the order given.
// kv alternates string keys and values. A time.Duration is rendered as whole
// milliseconds, other values with fmt. Values that are empty or contain a
// space, '=', '"' or a control character are quoted with strconv.Quote.
func Format(t time.Time, event string, kv ...any) string {
	var b strings.Builder
	b.WriteString("ts=")
	b.WriteString(t.Format("2006-01-02T15:04:05Z07:00"))
	b.WriteString(" event=")
	b.WriteString(value(event))
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteByte(' ')
		b.WriteString(value(fmt.Sprint(kv[i])))
		b.WriteByte('=')
		switch v := kv[i+1].(type) {
		case time.Duration:
			b.WriteString(strconv.FormatInt(v.Milliseconds(), 10))
		case string:
			b.WriteString(value(v))
		default:
			b.WriteString(value(fmt.Sprint(v)))
		}
	}
	return b.String()
}

func value(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r == ' ' || r == '=' || r == '"' || r < 0x20 || r == 0x7f {
			return strconv.Quote(s)
		}
	}
	return s
}
