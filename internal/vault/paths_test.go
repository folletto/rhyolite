package vault

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	ok := map[string]string{
		"":             "",
		"/":            "",
		".":            "",
		"a.md":         "a.md",
		"a/b/c.md":     "a/b/c.md",
		"a//b/./c.md":  "a/b/c.md",
		"a/../b.md":    "b.md",
		"notes/é ü.md": "notes/é ü.md",
		"日本語/ノート.md":   "日本語/ノート.md",
	}
	for in, want := range ok {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"..", "../a.md", "/a.md", "//a.md", "/a/b.md", "a/../../b.md", "a/../..", "/../etc/passwd", "../../etc/passwd",
		"a\x00b.md", `a\b.md`, `..\a.md`, "C:/x.md", "c:x.md",
		".git/config", "a/.hidden/b.md", ".trash/a.md", ".rhyolite-tmp-1", "a/.md",
		strings.Repeat("a", 300) + ".md", strings.Repeat("a/", 3000),
	}
	for _, in := range bad {
		if got, err := Clean(in); err == nil {
			t.Errorf("Clean(%q) = %q; want error", in, got)
		}
	}
}

func TestCleanFileRequiresMarkdown(t *testing.T) {
	for _, in := range []string{"a.txt", "rhyolite.json", "rhyolite.log", "a.md.bak", "", "/", "dir/", "a.MDX", "md"} {
		if _, err := CleanFile(in); err == nil {
			t.Errorf("CleanFile(%q) accepted", in)
		}
	}
	for _, in := range []string{"a.md", "A.MD", "x/y.Md"} {
		if _, err := CleanFile(in); err != nil {
			t.Errorf("CleanFile(%q): %v", in, err)
		}
	}
}

func FuzzClean(f *testing.F) {
	for _, s := range []string{"a.md", "../x", "a/../../b", "/etc/passwd", "a\x00", `a\..\b`, "....//x", ".//..//x", "a/./b/../../../c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := Clean(in)
		if err != nil {
			return
		}
		if got == "" {
			return
		}
		if filepath.IsAbs(got) || strings.HasPrefix(got, "/") || !filepath.IsLocal(got) {
			t.Fatalf("Clean(%q) = %q escapes", in, got)
		}
		for _, seg := range strings.Split(got, "/") {
			if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
				t.Fatalf("Clean(%q) = %q has bad segment %q", in, got, seg)
			}
		}
	})
}
