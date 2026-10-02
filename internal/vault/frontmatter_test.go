package vault

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		in, raw, rest string
		found         bool
	}{
		{"---\na: 1\n---\nbody\n", "a: 1\n", "body\n", true},
		{"---\n---\nbody", "", "body", true},
		{"---\na: 1\n...\nbody", "a: 1\n", "body", true},
		{"---\na: 1\n---", "a: 1\n", "", true},
		{"no front\n---\na: 1\n---\n", "", "no front\n---\na: 1\n---\n", false},
		{"---\nunclosed: 1\nbody", "", "---\nunclosed: 1\nbody", false},
		{"", "", "", false},
		{"--- \na: 1\n--- \nx", "a: 1\n", "x", true},
	}
	for _, c := range cases {
		raw, rest, found := splitFrontmatter(c.in)
		if raw != c.raw || rest != c.rest || found != c.found {
			t.Errorf("split(%q) = %q,%q,%v; want %q,%q,%v", c.in, raw, rest, found, c.raw, c.rest, c.found)
		}
	}
}

func TestApplyFields(t *testing.T) {
	in := "---\n# keep me\ntitle: Hello\ntags: [a, b]   # inline\nold: x\n---\n# Body\n"
	out, changed, err := applyFields(in, map[string]any{"title": "New", "old": "", "status": "draft", "n": float64(3)})
	mustOK(t, err)
	if !changed {
		t.Fatal("not changed")
	}
	for _, want := range []string{"# keep me", "title: New", "tags: [a, b]", "# inline", "status: draft", "n: 3", "---\n# Body\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old:") {
		t.Errorf("deleted key still present:\n%s", out)
	}
	if strings.Index(out, "title:") > strings.Index(out, "tags:") {
		t.Errorf("key order changed:\n%s", out)
	}
}

func TestApplyFieldsCreatesBlockAndDeletesNoop(t *testing.T) {
	out, changed, err := applyFields("# Just body\n", map[string]any{"a": "b"})
	mustOK(t, err)
	if !changed || out != "---\na: b\n---\n# Just body\n" {
		t.Errorf("out = %q", out)
	}
	out, changed, err = applyFields("# Just body\n", map[string]any{"a": ""})
	mustOK(t, err)
	if changed || out != "# Just body\n" {
		t.Errorf("delete on missing block changed the file: %q", out)
	}
	out, _, _ = applyFields("---\na: 1\n---\nx\n", map[string]any{"a": nil})
	if out != "---\n---\nx\n" {
		t.Errorf("deleting the last key = %q", out)
	}
}

func TestApplyFieldsStructuredAndErrors(t *testing.T) {
	out, _, err := applyFields("x\n", map[string]any{"tags": []any{"a", "b"}, "meta": map[string]any{"k": "v"}})
	mustOK(t, err)
	m := frontmatterOf(out)
	if !reflect.DeepEqual(m["tags"], []any{"a", "b"}) || !reflect.DeepEqual(m["meta"], map[string]any{"k": "v"}) {
		t.Errorf("round trip = %#v", m)
	}
	if _, _, err := applyFields("---\n- a\n- b\n---\nx", map[string]any{"a": "b"}); err == nil {
		t.Error("list frontmatter accepted")
	}
	if _, _, err := applyFields("---\na: [unclosed\n---\nx", map[string]any{"a": "b"}); err == nil {
		t.Error("invalid YAML accepted")
	}
}

func TestMetaReadAndWrite(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "n.md", "---\r\ntitle: T\r\ndate: 2026-01-02\r\n---\r\nbody\r\nmore\r\n")

	r, err := v.MetaRead("s", "n.md")
	mustOK(t, err)
	if r.Frontmatter["title"] != "T" || r.Hash == "" {
		t.Fatalf("meta = %+v", r)
	}
	w, err := v.MetaWrite("s", "n.md", map[string]any{"title": "New", "date": "", "tag": "x"}, "")
	mustOK(t, err)
	if w.Frontmatter["title"] != "New" || w.Frontmatter["tag"] != "x" || w.Frontmatter["date"] != nil {
		t.Fatalf("after write: %+v", w.Frontmatter)
	}
	got := disk(t, dir, "n.md")
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("CRLF not preserved: %q", got)
	}
	if !strings.HasSuffix(got, "---\r\nbody\r\nmore\r\n") {
		t.Errorf("body changed: %q", got)
	}
}

func TestMetaReadEdgeCases(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "none.md", "# no frontmatter\n")
	put(t, dir, "bom.md", "\xef\xbb\xbf---\na: 1\n---\nx")
	put(t, dir, "bad.md", "---\na: [oops\n---\nx")
	r, _ := v.MetaRead("s", "none.md")
	if r.Frontmatter != nil {
		t.Errorf("frontmatter = %v", r.Frontmatter)
	}
	r, _ = v.MetaRead("s", "bom.md")
	if r.Frontmatter["a"] != 1 {
		t.Errorf("BOM file frontmatter = %v", r.Frontmatter)
	}
	r, err := v.MetaRead("s", "bad.md")
	mustOK(t, err)
	if r.FrontmatterError == "" {
		t.Error("malformed frontmatter not reported")
	}
	if _, err := v.MetaWrite("s", "bad.md", map[string]any{"a": "b"}, ""); err == nil {
		t.Error("meta write on malformed frontmatter succeeded")
	}
	// BOM is kept on rewrite.
	_, err = v.MetaWrite("s", "bom.md", map[string]any{"b": "2"}, "")
	mustOK(t, err)
	if !strings.HasPrefix(disk(t, dir, "bom.md"), "\xef\xbb\xbf---\n") {
		t.Errorf("BOM lost: %q", disk(t, dir, "bom.md"))
	}
}
