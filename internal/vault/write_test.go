package vault

import (
	"strings"
	"sync"
	"testing"
)

func TestWriteCreatesWithParentsAndRefusesExisting(t *testing.T) {
	v, dir, _ := newVault(t)
	r, err := v.Write("s", "a/b/c.md", "hello\n", false, "")
	mustOK(t, err)
	if !r.Created || disk(t, dir, "a/b/c.md") != "hello\n" || r.Hash != hashBytes([]byte("hello\n")) {
		t.Fatalf("%+v", r)
	}
	_, err = v.Write("s", "a/b/c.md", "other", false, "")
	wantErr(t, err, "already exists")
	if disk(t, dir, "a/b/c.md") != "hello\n" {
		t.Error("existing file modified")
	}
	_, err = v.Write("s", "a/b/c.md/d.md", "x", false, "")
	if err == nil {
		t.Error("wrote below a file")
	}
	_, err = v.Write("s", "bad.md", "\xff", false, "")
	wantErr(t, err, "UTF-8")
	_, err = v.Write("s", "nul.md", "a\x00b", false, "")
	wantErr(t, err, "NUL")
	if exists(dir, "bad.md") || exists(dir, "nul.md") {
		t.Error("invalid content was written")
	}
}

func TestWriteOverwrite(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "old")
	r, err := v.Write("s", "a.md", "new", true, "")
	mustOK(t, err)
	if r.Created || disk(t, dir, "a.md") != "new" {
		t.Fatalf("%+v", r)
	}
	// overwrite=true on a new file simply creates it
	r, err = v.Write("s", "fresh.md", "x", true, "")
	mustOK(t, err)
	if !r.Created {
		t.Fatal("not created")
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	v, dir, _ := newVault(t)
	v.Write("s", "a.md", "x", false, "")
	v.Write("s", "a.md", "y", true, "")
	v.Append("s", "a.md", "z")
	v.Patch("s", "a.md", "y", "w", false, "")
	v.MetaWrite("s", "a.md", map[string]any{"k": "v"}, "")
	entries, _ := listDisk(dir)
	if len(entries) != 1 || entries[0] != "a.md" {
		t.Fatalf("directory = %v", entries)
	}
}

func TestAppend(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "one")
	_, err := v.Append("s", "a.md", "two\n")
	mustOK(t, err)
	if disk(t, dir, "a.md") != "one\ntwo\n" {
		t.Errorf("got %q", disk(t, dir, "a.md"))
	}
	_, err = v.Append("s", "a.md", "three\n")
	mustOK(t, err)
	if disk(t, dir, "a.md") != "one\ntwo\nthree\n" {
		t.Errorf("got %q", disk(t, dir, "a.md"))
	}
	put(t, dir, "crlf.md", "a\r\nb")
	v.Append("s", "crlf.md", "c\nd\n")
	if disk(t, dir, "crlf.md") != "a\r\nb\r\nc\r\nd\r\n" {
		t.Errorf("CRLF got %q", disk(t, dir, "crlf.md"))
	}
	put(t, dir, "empty.md", "")
	v.Append("s", "empty.md", "first")
	if disk(t, dir, "empty.md") != "first" {
		t.Errorf("empty got %q", disk(t, dir, "empty.md"))
	}
	_, err = v.Append("s", "missing.md", "x")
	wantErr(t, err, "not found")
}

func TestPatchRules(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "alpha beta\nbeta gamma\n")

	_, err := v.Patch("s", "a.md", "", "x", false, "")
	wantErr(t, err, "must not be empty")
	_, err = v.Patch("s", "a.md", "alpha", "alpha", false, "")
	wantErr(t, err, "identical")
	_, err = v.Patch("s", "a.md", "zzz", "x", false, "")
	wantErr(t, err, "not found")
	_, err = v.Patch("s", "a.md", "beta", "B", false, "")
	wantErr(t, err, "appears 2 times")
	if disk(t, dir, "a.md") != "alpha beta\nbeta gamma\n" {
		t.Fatal("file changed by failed patch")
	}
	r, err := v.Patch("s", "a.md", "alpha", "ALPHA", false, "")
	mustOK(t, err)
	if r.Replacements != 1 || disk(t, dir, "a.md") != "ALPHA beta\nbeta gamma\n" {
		t.Fatalf("%+v %q", r, disk(t, dir, "a.md"))
	}
	r, err = v.Patch("s", "a.md", "beta", "B", true, "")
	mustOK(t, err)
	if r.Replacements != 2 || disk(t, dir, "a.md") != "ALPHA B\nB gamma\n" {
		t.Fatalf("%+v %q", r, disk(t, dir, "a.md"))
	}
	// multi-line, and deletion with empty new_string
	_, err = v.Patch("s", "a.md", "B\nB gamma\n", "", false, "")
	mustOK(t, err)
	if disk(t, dir, "a.md") != "ALPHA " {
		t.Errorf("got %q", disk(t, dir, "a.md"))
	}
	_, err = v.Patch("s", "a.md", "ALPHA", "\xff", false, "")
	wantErr(t, err, "UTF-8")
	_, err = v.Patch("s", "missing.md", "a", "b", false, "")
	wantErr(t, err, "not found")
}

func TestPatchKeepsCRLFAndBOM(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "\xef\xbb\xbfline one\r\nline two\r\nline three\r\n")
	_, err := v.Patch("s", "a.md", "line one\nline two", "first\nsecond\nextra", false, "")
	mustOK(t, err)
	want := "\xef\xbb\xbffirst\r\nsecond\r\nextra\r\nline three\r\n"
	if disk(t, dir, "a.md") != want {
		t.Errorf("got %q want %q", disk(t, dir, "a.md"), want)
	}
}

func TestWritesPreserveFileMode(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "x")
	chmod(t, dir+"/a.md", 0o600)
	v.Patch("s", "a.md", "x", "y", false, "")
	if m := modeOf(t, dir+"/a.md"); m != 0o600 {
		t.Errorf("mode = %o", m)
	}
}

func TestConcurrentAppendsAreAllKept(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "log.md", "start\n")
	var wg sync.WaitGroup
	const n = 40
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Append("s", "log.md", "entry\n"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := strings.Count(disk(t, dir, "log.md"), "entry\n"); got != n {
		t.Fatalf("%d entries, want %d", got, n)
	}
}

func TestConcurrentPatchesOfSameStringExactlyOneWins(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "TOKEN\n")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Patch("s", "a.md", "TOKEN", "done", false, ""); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || disk(t, dir, "a.md") != "done\n" {
		t.Fatalf("%d successes, file %q", ok, disk(t, dir, "a.md"))
	}
}
