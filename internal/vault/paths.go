package vault

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxPathLen    = 4096
	maxSegmentLen = 255
)

var volumeRe = regexp.MustCompile(`^[A-Za-z]:`)

// Clean validates a client-supplied path and returns it vault-relative with
// "/" separators. "" and "/" are the vault root; any other path starting
// with "/" is absolute and rejected.
//
// Rejected: NUL, backslashes, ".." escaping the root, Windows volumes, and
// any segment starting with "." (hidden: .git, .trash, temp files...).
func Clean(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", errf("invalid path: contains NUL")
	}
	if strings.Contains(p, `\`) {
		return "", errf("invalid path: use / as separator")
	}
	if len(p) > maxPathLen {
		return "", errf("invalid path: too long")
	}
	if strings.HasPrefix(p, "/") {
		if strings.Trim(p, "/") == "" {
			return "", nil
		}
		return "", errf("invalid path: use paths relative to the vault root, without a leading /")
	}
	if p == "" || p == "." {
		return "", nil
	}
	c := path.Clean(p)
	if c == "." {
		return "", nil
	}
	if c == ".." || strings.HasPrefix(c, "../") || !filepath.IsLocal(filepath.FromSlash(c)) {
		return "", errf("invalid path: outside the vault")
	}
	segs := strings.Split(c, "/")
	if volumeRe.MatchString(segs[0]) {
		return "", errf("invalid path: %q", p)
	}
	for _, s := range segs {
		if len(s) > maxSegmentLen {
			return "", errf("invalid path: name too long")
		}
		if strings.HasPrefix(s, ".") {
			return "", errf("invalid path: names starting with '.' are not accessible")
		}
	}
	return c, nil
}

// IsMarkdown reports whether name ends in .md (any case).
func IsMarkdown(name string) bool {
	return len(name) > 3 && strings.EqualFold(name[len(name)-3:], ".md")
}

// CleanFile is Clean for a markdown file path.
func CleanFile(p string) (string, error) {
	c, err := Clean(p)
	if err != nil {
		return "", err
	}
	if c == "" {
		return "", errf("path must be a .md file, not the vault root")
	}
	if !IsMarkdown(c) {
		return "", errf("only .md files are accessible: %q", c)
	}
	return c, nil
}

// CleanFolder is Clean for a folder path that must not be the root.
func cleanFolderNotRoot(p string) (string, error) {
	c, err := Clean(p)
	if err != nil {
		return "", err
	}
	if c == "" {
		return "", errf("the vault root cannot be used here")
	}
	return c, nil
}

func parentOf(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

// cmpPath orders paths component by component (pre-order tree order).
func cmpPath(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := strings.Compare(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return len(as) - len(bs)
}

func isWithin(rel, dir string) bool { return dir == "" || strings.HasPrefix(rel, dir+"/") }
