// Package vault implements every file operation behind the MCP tools.
//
// All access goes through an os.Root, so the operating system refuses any
// path that would leave the vault folder. On top of that, only Markdown
// files and folders are visible, symlinks are skipped, dot-names are hidden,
// and errors never reveal host paths.
package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/folletto/rhyolite/internal/trash"
)

// MaxFileBytes is the largest file the server will rewrite.
const MaxFileBytes = 10 << 20

// Vault is a folder of Markdown files.
type Vault struct {
	root  *os.Root
	abs   string // resolved absolute root; never leaves this package
	trash trash.Trasher

	mu  sync.Mutex // serialises every mutation (checks and writes are atomic together)
	trk *tracker
	now func() time.Time
}

// Open opens dir as a vault. t may be nil to use the system trash.
func Open(dir string, t trash.Trasher) (*Vault, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &Error{"not a directory"}
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	if t == nil {
		t = trash.System()
	}
	return &Vault{root: r, abs: abs, trash: t, trk: newTracker(), now: time.Now}, nil
}

// Close releases the root.
func (v *Vault) Close() error { return v.root.Close() }

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:16]
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// dot converts "" (root) to ".", the name os.Root expects.
func dot(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

// lstat is Lstat that also rejects symlinks anywhere along the path
// (os.Root alone would follow links that stay inside the vault).
func (v *Vault) lstat(rel string) (fs.FileInfo, error) {
	if rel == "" {
		return v.root.Lstat(".")
	}
	var info fs.FileInfo
	cur := ""
	for _, seg := range strings.Split(rel, "/") {
		if cur != "" {
			cur += "/"
		}
		cur += seg
		var err error
		if info, err = v.root.Lstat(cur); err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, errNotFound(rel)
		}
	}
	return info, nil
}

// checkParents makes sure existing ancestors of rel are real folders.
func (v *Vault) checkParents(rel string) error {
	parent := parentOf(rel)
	if parent == "" {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(parent, "/") {
		if cur != "" {
			cur += "/"
		}
		cur += seg
		info, err := v.root.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return errf("a parent of %q is not a folder", rel)
		}
	}
	return nil
}

func (v *Vault) mkParents(rel string) error {
	if err := v.checkParents(rel); err != nil {
		return err
	}
	if p := parentOf(rel); p != "" {
		return v.root.MkdirAll(p, 0o755)
	}
	return nil
}

// existingFile returns info for an existing regular .md file.
func (v *Vault) existingFile(rel string) (fs.FileInfo, error) {
	info, err := v.lstat(rel)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotFound(rel)
	}
	return info, nil
}

func (v *Vault) readAll(rel string) ([]byte, fs.FileInfo, error) {
	info, err := v.existingFile(rel)
	if err != nil {
		return nil, nil, err
	}
	if info.Size() > MaxFileBytes {
		return nil, nil, errf("file is larger than %d MB; use file_read with offset/limit", MaxFileBytes>>20)
	}
	b, err := v.root.ReadFile(rel)
	return b, info, err
}

// writeAtomic writes data to rel through a temp file in the same folder.
func (v *Vault) writeAtomic(rel string, data []byte, perm fs.FileMode) error {
	var rnd [6]byte
	rand.Read(rnd[:])
	tmp := path2(parentOf(rel), ".rhyolite-tmp-"+hex.EncodeToString(rnd[:]))
	f, err := v.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		v.root.Remove(tmp)
		return werr
	}
	if err := v.root.Rename(tmp, rel); err != nil {
		v.root.Remove(tmp)
		return err
	}
	return nil
}

func path2(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func isNotExist(err error) bool { return os.IsNotExist(err) }
