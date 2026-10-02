package vault

import "testing"

// One test per row of the table in PLAN.md §7.

func TestStaleEditOnUnreadFileIsAllowed(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "hello world\n")
	r, err := v.Patch("s1", "a.md", "hello", "bye", false, "")
	mustOK(t, err)
	if r.Notice != "" {
		t.Errorf("unexpected notice %q", r.Notice)
	}
	_, err = v.MetaWrite("s1", "a.md", map[string]any{"k": "v"}, "")
	mustOK(t, err)
}

func TestPatchAfterFileChangedGivesNoticeButApplies(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "first\n\nsecond\n")
	_, err := v.Read("s1", "a.md", 0, 0)
	mustOK(t, err)
	// Another client edits a different part.
	_, err = v.Patch("s2", "a.md", "first", "FIRST", false, "")
	mustOK(t, err)

	r, err := v.Patch("s1", "a.md", "second", "SECOND", false, "")
	mustOK(t, err)
	if r.Notice == "" {
		t.Error("expected a stale notice")
	}
	if disk(t, dir, "a.md") != "FIRST\n\nSECOND\n" {
		t.Errorf("got %q", disk(t, dir, "a.md"))
	}
	// The notice stays until the session reads the file again.
	r, _ = v.Patch("s1", "a.md", "SECOND", "second", false, "")
	if r.Notice == "" {
		t.Error("notice should persist until re-read")
	}
	_, err = v.Read("s1", "a.md", 0, 0)
	mustOK(t, err)
	r, _ = v.Patch("s1", "a.md", "second", "again", false, "")
	if r.Notice != "" {
		t.Errorf("notice after re-read: %q", r.Notice)
	}
}

func TestStalePatchWithMissingTextMentionsTheChange(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "text one\n")
	v.Read("s1", "a.md", 0, 0)
	v.Patch("s2", "a.md", "text one", "text two", false, "")
	_, err := v.Patch("s1", "a.md", "text one", "x", false, "")
	wantErr(t, err, "read it again")
}

func TestMetaWriteOnChangedFileAppliesToCurrentFrontmatter(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "---\na: 1\n---\nx\n")
	v.MetaRead("s1", "a.md")
	v.MetaWrite("s2", "a.md", map[string]any{"b": "2"}, "")
	r, err := v.MetaWrite("s1", "a.md", map[string]any{"c": "3"}, "")
	mustOK(t, err)
	if r.Notice == "" || r.Frontmatter["b"] != "2" || r.Frontmatter["c"] != "3" || r.Frontmatter["a"] != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestOverwriteRules(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", lines(2500))

	// never read: allowed
	_, err := v.Write("s0", "a.md", "x\n", true, "")
	mustOK(t, err)

	// read in full, unchanged: allowed
	put(t, dir, "b.md", "one\n")
	v.Read("s1", "b.md", 0, 0)
	_, err = v.Write("s1", "b.md", "two\n", true, "")
	mustOK(t, err)

	// partial read: refused
	put(t, dir, "big.md", lines(2500))
	v.Read("s1", "big.md", 0, 0) // truncated at 2000 lines
	_, err = v.Write("s1", "big.md", "x", true, "")
	wantErr(t, err, "partially read")
	v.Read("s1", "b.md", 1, 1) // a range starting at 1 that covers the file is full
	v.MetaRead("s1", "b.md")   // meta read is partial
	_, err = v.Write("s1", "b.md", "three\n", true, "")
	wantErr(t, err, "partially read")

	// changed since read: refused
	put(t, dir, "c.md", "one\n")
	v.Read("s1", "c.md", 0, 0)
	v.Append("s2", "c.md", "two\n")
	_, err = v.Write("s1", "c.md", "mine\n", true, "")
	wantErr(t, err, "modified since")
	if disk(t, dir, "c.md") != "one\ntwo\n" {
		t.Error("file overwritten despite error")
	}
	// re-read, then it works
	v.Read("s1", "c.md", 0, 0)
	_, err = v.Write("s1", "c.md", "mine\n", true, "")
	mustOK(t, err)
}

func TestExpectedHashIsStrict(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "hello\n")
	r, _ := v.Read("s1", "a.md", 0, 0)

	// someone else changes it
	v.Append("s2", "a.md", "more\n")
	if _, err := v.Patch("s1", "a.md", "hello", "bye", false, r.Hash); err == nil {
		t.Error("patch with stale hash succeeded")
	}
	if _, err := v.MetaWrite("s1", "a.md", map[string]any{"k": "v"}, r.Hash); err == nil {
		t.Error("meta write with stale hash succeeded")
	}
	if _, err := v.Write("s1", "a.md", "x", true, r.Hash); err == nil {
		t.Error("overwrite with stale hash succeeded")
	}
	// with the current hash it works, even from a session that never read it
	cur, _ := v.MetaRead("s3", "a.md")
	w, err := v.Patch("s4", "a.md", "hello", "bye", false, cur.Hash)
	mustOK(t, err)
	// and the returned hash chains
	if _, err := v.Patch("s4", "a.md", "bye", "ciao", false, w.Hash); err != nil {
		t.Fatal(err)
	}
}

func TestChainedWritesNeedNoReread(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "a b c\n")
	v.Read("s1", "a.md", 0, 0)
	for _, p := range [][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		r, err := v.Patch("s1", "a.md", p[0], p[1], false, "")
		mustOK(t, err)
		if r.Notice != "" {
			t.Fatalf("notice on own edit: %q", r.Notice)
		}
	}
	// full overwrite still allowed after own edits
	_, err := v.Write("s1", "a.md", "fresh\n", true, "")
	mustOK(t, err)
}

func TestSessionsAreIsolated(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "one\n")
	v.Read("s1", "a.md", 0, 0)
	v.Append("s1", "a.md", "two\n") // s1's own append keeps it current
	r, _ := v.Patch("s1", "a.md", "one", "ONE", false, "")
	if r.Notice != "" {
		t.Errorf("own append produced notice: %q", r.Notice)
	}
	// s2 never read anything: no notice, no block
	r, err := v.Patch("s2", "a.md", "two", "TWO", false, "")
	mustOK(t, err)
	if r.Notice != "" {
		t.Errorf("s2 notice: %q", r.Notice)
	}
}

func TestAppendAndCreateAreNeverBlocked(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "one\n")
	v.Read("s1", "a.md", 0, 0)
	v.Append("s2", "a.md", "two\n")
	if _, err := v.Append("s1", "a.md", "three\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Write("s1", "new.md", "x", false, ""); err != nil {
		t.Fatal(err)
	}
}
