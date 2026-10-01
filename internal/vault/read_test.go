package vault

import (
	"fmt"
	"strings"
	"testing"
)

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func TestReadWholeFile(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "---\ntitle: Hi\n---\nbody\n")
	r, err := v.Read("s", "a.md", 0, 0)
	mustOK(t, err)
	if r.Frontmatter["title"] != "Hi" || r.TotalLines != 4 || r.LinesReturned != 4 || r.Truncated || r.Offset != 1 {
		t.Fatalf("%+v", r)
	}
	if r.Content != "---\ntitle: Hi\n---\nbody\n" || r.LineEnding != "lf" || r.Hash == "" {
		t.Fatalf("%+v", r)
	}
}

func TestReadLinesCapAndPaging(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "big.md", lines(2500))
	r, err := v.Read("s", "big.md", 0, 0)
	mustOK(t, err)
	if r.LinesReturned != ReadMaxLines || !r.Truncated || r.NextOffset != 2001 || r.TotalLines != 2500 {
		t.Fatalf("%+v", r)
	}
	r2, err := v.Read("s", "big.md", r.NextOffset, 0)
	mustOK(t, err)
	if r2.LinesReturned != 500 || r2.Truncated || !strings.HasPrefix(r2.Content, "line 2001\n") {
		t.Fatalf("%+v", r2)
	}
	r3, _ := v.Read("s", "big.md", 10, 3)
	if r3.Content != "line 10\nline 11\nline 12\n" || r3.NextOffset != 13 || !r3.Truncated {
		t.Fatalf("%+v", r3)
	}
	if r3.Hash != r.Hash {
		t.Error("hash must describe the whole file, not the range")
	}
	r4, _ := v.Read("s", "big.md", 9999, 5)
	if r4.LinesReturned != 0 || r4.Content != "" || r4.TotalLines != 2500 {
		t.Fatalf("%+v", r4)
	}
}

func TestReadRefusesLargeFilesWithoutRange(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "huge.md", strings.Repeat("0123456789abcdef\n", 20000)) // 340 KB, 20000 lines
	_, err := v.Read("s", "huge.md", 0, 0)
	wantErr(t, err, "offset and limit")
	r, err := v.Read("s", "huge.md", 1, 100)
	mustOK(t, err)
	if r.LinesReturned != 100 || r.TotalLines != 20000 {
		t.Fatalf("%+v", r)
	}
}

func TestReadByteCapStopsEarly(t *testing.T) {
	v, dir, _ := newVault(t)
	long := strings.Repeat("x", 1900)
	put(t, dir, "wide.md", strings.Repeat(long+"\n", 300)) // 570 KB; 2000 lines max, 256 KB output max
	r, err := v.Read("s", "wide.md", 1, 2000)
	mustOK(t, err)
	if !r.Truncated || len(r.Content) > ReadMaxBytes+10 || r.NextOffset == 0 || r.NextOffset != r.LinesReturned+1 {
		t.Fatalf("lines=%d next=%d bytes=%d truncated=%v", r.LinesReturned, r.NextOffset, len(r.Content), r.Truncated)
	}
}

func TestReadTruncatesLongLines(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "l.md", strings.Repeat("é", 3000)+"\nshort\n")
	r, err := v.Read("s", "l.md", 0, 0)
	mustOK(t, err)
	first := strings.SplitN(r.Content, "\n", 2)[0]
	if !strings.HasSuffix(first, "[line truncated]") || len(first) > ReadMaxLineLen+30 {
		t.Fatalf("line not truncated: %d bytes", len(first))
	}
	if r.TotalLines != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestReadLineEndingsAndBOM(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "c.md", "\xef\xbb\xbf---\r\na: 1\r\n---\r\nx\r\ny")
	r, err := v.Read("s", "c.md", 0, 0)
	mustOK(t, err)
	if r.LineEnding != "crlf" || r.Content != "---\na: 1\n---\nx\ny\n" || r.Frontmatter["a"] != 1 || r.TotalLines != 5 {
		t.Fatalf("%+v", r)
	}
}

func TestReadInvalidUTF8AndMissing(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "bin.md", "ok\n\xff\xfe broken\n")
	_, err := v.Read("s", "bin.md", 0, 0)
	wantErr(t, err, "UTF-8")
	_, err = v.Read("s", "nope.md", 0, 0)
	wantErr(t, err, "not found")
	put(t, dir, "folder.md/x.md", "x")
	_, err = v.Read("s", "folder.md", 0, 0)
	wantErr(t, err, "not found")
	put(t, dir, "empty.md", "")
	r, err := v.Read("s", "empty.md", 0, 0)
	mustOK(t, err)
	if r.TotalLines != 0 || r.Content != "" {
		t.Fatalf("%+v", r)
	}
}
