package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/folletto/rhyolite/internal/trash"
)

type fakeTrash struct {
	got []string
	err error
}

func (f *fakeTrash) Trash(abs string) error {
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, abs)
	return os.RemoveAll(abs) // emulate "moved away"
}

func newVault(t *testing.T) (*Vault, string, *fakeTrash) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeTrash{}
	v, err := Open(dir, ft)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v, dir, ft
}

// put writes a file straight to disk, bypassing the vault.
func put(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func disk(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(dir, rel string) bool {
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err, substr)
	}
}

// noLeak fails if an error message reveals the host directory.
func noLeak(t *testing.T, dir string, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), dir) {
		t.Fatalf("error leaks host path: %v", err)
	}
}

var errNoTrash = trash.ErrUnavailable

func isErr(err, target error) bool { return errors.Is(err, target) }
