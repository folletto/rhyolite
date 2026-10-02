package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// A vault next to a "secret" folder it must never touch.
func vaultWithOutside(t *testing.T) (v *Vault, dir, outside string) {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	dir = filepath.Join(base, "vault")
	outside = filepath.Join(base, "outside")
	os.MkdirAll(dir, 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(outside, "secret.md"), []byte("SECRET"), 0o644)
	var err error
	if v, err = Open(dir, &fakeTrash{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return
}

func TestTraversalIsRejectedEverywhere(t *testing.T) {
	v, dir, outside := vaultWithOutside(t)
	evil := []string{"../outside/secret.md", "a/../../outside/secret.md", "/../outside/secret.md", outside + "/secret.md"}
	for _, p := range evil {
		_, err := v.Read("s", p, 0, 0)
		if err == nil {
			t.Errorf("Read(%q) succeeded", p)
		}
		noLeak(t, dir, err)
		if _, err := v.MetaRead("s", p); err == nil {
			t.Errorf("MetaRead(%q) succeeded", p)
		}
		if _, err := v.Write("s", p, "x", true, ""); err == nil {
			t.Errorf("Write(%q) succeeded", p)
		}
		if _, err := v.Append("s", p, "x"); err == nil {
			t.Errorf("Append(%q) succeeded", p)
		}
		if _, err := v.Patch("s", p, "SECRET", "x", false, ""); err == nil {
			t.Errorf("Patch(%q) succeeded", p)
		}
		if _, err := v.Delete(p); err == nil {
			t.Errorf("Delete(%q) succeeded", p)
		}
		if _, err := v.Move(p, "stolen.md"); err == nil {
			t.Errorf("Move from %q succeeded", p)
		}
		if _, err := v.Move("a.md", p); err == nil {
			t.Errorf("Move to %q succeeded", p)
		}
		if _, err := v.List(p, All); err == nil {
			t.Errorf("List(%q) succeeded", p)
		}
		if _, err := v.FolderCreate(p + "x"); err == nil {
			t.Errorf("FolderCreate(%q) succeeded", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "secret.md")); string(b) != "SECRET" {
		t.Fatal("outside file was modified")
	}
}

func TestSymlinksAreSkippedAndRejected(t *testing.T) {
	v, dir, outside := vaultWithOutside(t)
	put(t, dir, "real/in.md", "inside")
	os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "link-out.md")) // file link, outside
	os.Symlink(outside, filepath.Join(dir, "dir-out"))                                 // dir link, outside
	os.Symlink("real/in.md", filepath.Join(dir, "link-in.md"))                         // file link, inside
	os.Symlink("real", filepath.Join(dir, "dir-in"))                                   // dir link, inside

	list, err := v.List("", All)
	mustOK(t, err)
	if len(list) != 1 || list[0].Name != "real" {
		t.Fatalf("symlinks leaked into listing: %+v", list)
	}
	rec, _ := v.ListRecursive("", All, 0, 0, "")
	if len(rec.Items) != 2 { // real, real/in.md
		t.Fatalf("recursive listing = %+v", rec.Items)
	}
	for _, p := range []string{"link-out.md", "link-in.md", "dir-out/secret.md", "dir-in/in.md"} {
		if _, err := v.Read("s", p, 0, 0); err == nil {
			t.Errorf("Read through symlink %q succeeded", p)
		}
		if _, err := v.Write("s", p, "x", true, ""); err == nil {
			t.Errorf("Write through symlink %q succeeded", p)
		}
		if _, err := v.Patch("s", p, "SECRET", "x", false, ""); err == nil {
			t.Errorf("Patch through symlink %q succeeded", p)
		}
		if _, err := v.Delete(p); err == nil {
			t.Errorf("Delete through symlink %q succeeded", p)
		}
	}
	if _, err := v.Write("s", "dir-out/new.md", "x", false, ""); err == nil {
		t.Error("created a file through a symlinked folder")
	}
	if exists(outside, "new.md") {
		t.Error("file created outside the vault")
	}
	res, err := v.Search(SearchOptions{Query: "SECRET"})
	mustOK(t, err)
	if len(res.Results) != 0 {
		t.Errorf("search followed symlinks: %+v", res.Results)
	}
}

func TestOnlyMarkdownAndNoDotfilesAreVisible(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "note.md", "x")
	put(t, dir, "rhyolite.json", `{"token":"secret-token"}`)
	put(t, dir, "rhyolite.log", "log")
	put(t, dir, "image.png", "png")
	put(t, dir, "notes.txt", "txt")
	put(t, dir, ".hidden.md", "x")
	put(t, dir, ".git/config.md", "x")
	put(t, dir, ".trash/old.md", "x")
	put(t, dir, "sub/readme.MD", "upper")
	put(t, dir, "sub/data.json", "{}")

	rec, err := v.ListRecursive("", All, 0, 0, "")
	mustOK(t, err)
	var got []string
	for _, e := range rec.Items {
		got = append(got, e.Path)
	}
	want := []string{"note.md", "sub", "sub/readme.MD"}
	if len(got) != len(want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("visible = %v, want %v", got, want)
		}
	}
	for _, p := range []string{"rhyolite.json", "rhyolite.log", "image.png", "notes.txt", ".hidden.md", ".git/config.md", ".trash/old.md", "sub/data.json"} {
		if _, err := v.Read("s", p, 0, 0); err == nil {
			t.Errorf("Read(%q) succeeded", p)
		}
		if _, err := v.MetaRead("s", p); err == nil {
			t.Errorf("MetaRead(%q) succeeded", p)
		}
		if _, err := v.Write("s", p, "x", true, ""); err == nil {
			t.Errorf("Write(%q) succeeded", p)
		}
		if _, err := v.Delete(p); err == nil {
			t.Errorf("Delete(%q) succeeded", p)
		}
	}
	for _, q := range []string{"secret-token", "png", "token"} {
		res, _ := v.Search(SearchOptions{Query: q})
		if len(res.Results) != 0 {
			t.Errorf("search %q exposed hidden content: %+v", q, res.Results)
		}
	}
	if disk(t, dir, "rhyolite.json") != `{"token":"secret-token"}` {
		t.Error("config file modified")
	}
	if _, err := v.Move("note.md", "rhyolite.json"); err == nil {
		t.Error("moved a note over rhyolite.json")
	}
}

func TestErrorsNeverLeakHostPath(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "file.md", "x")
	put(t, dir, "folder/a.md", "x")
	calls := []func() error{
		func() error { _, e := v.Read("s", "missing/x.md", 0, 0); return e },
		func() error { _, e := v.Write("s", "file.md/child.md", "x", false, ""); return e },
		func() error { _, e := v.Append("s", "nope.md", "x"); return e },
		func() error { _, e := v.List("file.md", All); return e },
		func() error { _, e := v.FolderDelete("folder"); return e },
		func() error { _, e := v.Move("file.md", "folder"); return e },
		func() error { _, e := v.FolderMove("folder", "file.md"); return e },
		func() error { _, e := v.Patch("s", "nope.md", "a", "b", false, ""); return e },
	}
	for i, c := range calls {
		err := c()
		if err == nil {
			t.Errorf("call %d: expected an error", i)
		}
		noLeak(t, dir, err)
	}
}
