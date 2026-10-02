package trash

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// freedesktop implements the freedesktop.org Trash specification (1.0).
// The home trash is used when the file lives on the same device; otherwise
// a per-volume trash ($topdir/.Trash/$uid or $topdir/.Trash-$uid).
type freedesktop struct {
	dataHome string                            // $XDG_DATA_HOME
	uid      int                               // current user
	device   func(path string) (uint64, error) // device ID of a path
	now      func() time.Time
}

func (f freedesktop) Trash(abs string) error {
	abs = filepath.Clean(abs)
	dev, err := f.device(abs)
	if err != nil {
		return err
	}
	home := filepath.Join(f.dataHome, "Trash")
	if err := os.MkdirAll(home, 0o700); err == nil {
		if hdev, err := f.device(home); err == nil && hdev == dev {
			return f.into(home, abs, abs)
		}
	}
	// Different volume: find its mount point (topdir).
	top := abs
	for {
		parent := filepath.Dir(top)
		if parent == top {
			break
		}
		pdev, err := f.device(parent)
		if err != nil || pdev != dev {
			break
		}
		top = parent
	}
	if top == abs { // the file itself is a mount point
		return ErrUnavailable
	}
	// $topdir/.Trash/$uid, only if .Trash is a real dir with the sticky bit.
	shared := filepath.Join(top, ".Trash")
	if info, err := os.Lstat(shared); err == nil && info.IsDir() && info.Mode()&os.ModeSticky != 0 && info.Mode()&fs.ModeSymlink == 0 {
		if err := os.MkdirAll(filepath.Join(shared, fmt.Sprint(f.uid)), 0o700); err == nil {
			return f.into(filepath.Join(shared, fmt.Sprint(f.uid)), abs, top)
		}
	}
	own := filepath.Join(top, fmt.Sprintf(".Trash-%d", f.uid))
	if err := os.MkdirAll(own, 0o700); err == nil {
		return f.into(own, abs, top)
	}
	return ErrUnavailable
}

// into moves abs into trashDir/files and writes trashDir/info/<name>.trashinfo.
// Paths in the info file are relative to base when the trash is on another
// volume, absolute for the home trash (base == abs means "absolute").
func (f freedesktop) into(trashDir, abs, base string) error {
	files, info := filepath.Join(trashDir, "files"), filepath.Join(trashDir, "info")
	for _, d := range []string{files, info} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return ErrUnavailable
		}
	}
	recorded := abs
	if base != abs {
		if rel, err := filepath.Rel(base, abs); err == nil {
			recorded = rel
		}
	}
	name := filepath.Base(abs)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s.%d%s", stem, i, ext)
		}
		infoPath := filepath.Join(info, cand+".trashinfo")
		// O_EXCL on the info file claims the name atomically.
		fh, err := os.OpenFile(infoPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := fmt.Fprintf(fh, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", encodePath(recorded), f.now().Format("2006-01-02T15:04:05"))
		fh.Close()
		if werr != nil {
			os.Remove(infoPath)
			return werr
		}
		if err := os.Rename(abs, filepath.Join(files, cand)); err != nil {
			os.Remove(infoPath)
			return err
		}
		return nil
	}
}

// encodePath percent-encodes a path as the spec requires, keeping slashes.
func encodePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
