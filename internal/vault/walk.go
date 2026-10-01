package vault

import (
	"io"
	"io/fs"
	"sort"
	"strings"
)

// Entry is one item in a listing.
type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"` // "file" or "folder"
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// Filter limits listings to files or folders.
type Filter string

const (
	All     Filter = "all"
	Files   Filter = "files"
	Folders Filter = "folders"
)

func (f Filter) valid() bool { return f == "" || f == All || f == Files || f == Folders }

func (f Filter) wants(isDir bool) bool {
	return f == "" || f == All || (f == Folders) == isDir
}

// visible returns the entries clients may see in a folder: sorted by name,
// without dot-names, symlinks, or non-markdown files.
func (v *Vault) visible(rel string) ([]fs.DirEntry, error) {
	if _, err := v.lstat(rel); err != nil {
		return nil, err
	}
	d, err := v.root.Open(dot(rel))
	if err != nil {
		return nil, err
	}
	defer d.Close()
	all, err := d.ReadDir(-1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	out := all[:0]
	for _, e := range all {
		n := e.Name()
		if strings.HasPrefix(n, ".") || e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		switch {
		case e.IsDir():
		case e.Type().IsRegular() && IsMarkdown(n):
		default:
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func toEntry(dir string, e fs.DirEntry) (Entry, bool) {
	info, err := e.Info()
	if err != nil {
		return Entry{}, false
	}
	en := Entry{Name: e.Name(), Path: path2(dir, e.Name()), Type: "file", Size: info.Size(), Modified: rfc3339(info.ModTime())}
	if e.IsDir() {
		en.Type, en.Size = "folder", 0
	}
	return en, true
}

// List returns the direct children of a folder.
func (v *Vault) List(rel string, filter Filter) ([]Entry, error) {
	rel, err := Clean(rel)
	if err != nil {
		return nil, err
	}
	if !filter.valid() {
		return nil, errf("type must be all, files or folders")
	}
	entries, err := v.visible(rel)
	if err != nil {
		return nil, v.sanitize(rel, err)
	}
	if info, _ := v.lstat(rel); info != nil && !info.IsDir() {
		return nil, errf("%q is not a folder", display(rel))
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !filter.wants(e.IsDir()) {
			continue
		}
		if en, ok := toEntry(rel, e); ok {
			out = append(out, en)
		}
	}
	return out, nil
}

// RecursiveResult is one page of a recursive listing.
type RecursiveResult struct {
	Items      []Entry `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

const (
	DefaultListLimit = 200
	MaxListLimit     = 1000
)

// walk visits entries below dir in stable pre-order (folders are visited
// right after their own entry). Entries at or before `after` are skipped, and
// whole subtrees that sort before it are never read, so resuming is cheap.
// maxDepth 0 means unlimited; depth 1 is the direct children of dir.
func (v *Vault) walk(dir, after string, maxDepth int, fn func(fs.DirEntry, string) bool) error {
	var rec func(dir string, depth int) (bool, error)
	rec = func(dir string, depth int) (bool, error) {
		entries, err := v.visible(dir)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			p := path2(dir, e.Name())
			c := 1
			if after != "" {
				c = cmpPath(p, after)
			}
			descend := e.IsDir() && (maxDepth == 0 || depth < maxDepth)
			if c > 0 {
				if !fn(e, p) {
					return false, nil
				}
			} else if e.IsDir() && c < 0 && !isWithin(after, p) {
				continue // whole subtree precedes the cursor
			}
			if descend {
				cont, err := rec(p, depth+1)
				if err != nil || !cont {
					return cont, err
				}
			}
		}
		return true, nil
	}
	_, err := rec(dir, 1)
	return err
}

// ListRecursive pages through everything below rel.
func (v *Vault) ListRecursive(rel string, filter Filter, maxDepth, limit int, cursor string) (*RecursiveResult, error) {
	rel, err := Clean(rel)
	if err != nil {
		return nil, err
	}
	if !filter.valid() {
		return nil, errf("type must be all, files or folders")
	}
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	after, err := decodeCursor(cursor, rel)
	if err != nil {
		return nil, err
	}
	res := &RecursiveResult{Items: []Entry{}}
	err = v.walk(rel, after, maxDepth, func(e fs.DirEntry, p string) bool {
		if !filter.wants(e.IsDir()) {
			return true
		}
		if len(res.Items) == limit {
			res.HasMore = true
			return false
		}
		en, ok := toEntry(parentOf(p), e)
		if ok {
			res.Items = append(res.Items, en)
		}
		return true
	})
	if err != nil {
		return nil, v.sanitize(rel, err)
	}
	if res.HasMore {
		res.NextCursor = encodeCursor(res.Items[len(res.Items)-1].Path)
	}
	return res, nil
}
