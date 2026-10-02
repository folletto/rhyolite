package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMoveAndRename(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "a.md", "A")
	r, err := v.Move("a.md", "new/deep/b.md") // creates folders and renames
	mustOK(t, err)
	if r.From != "a.md" || r.To != "new/deep/b.md" || exists(dir, "a.md") || disk(t, dir, "new/deep/b.md") != "A" {
		t.Fatalf("%+v", r)
	}
	put(t, dir, "x.md", "X")
	_, err = v.Move("x.md", "new/deep/b.md")
	wantErr(t, err, "already exists")
	if disk(t, dir, "new/deep/b.md") != "A" || disk(t, dir, "x.md") != "X" {
		t.Error("files changed by refused move")
	}
	_, err = v.Move("x.md", "x.md")
	wantErr(t, err, "same")
	_, err = v.Move("missing.md", "y.md")
	wantErr(t, err, "not found")
	_, err = v.Move("x.md", "x.txt")
	wantErr(t, err, ".md")
	_, err = v.Move("x.md", "new/deep/b.md/c.md")
	if err == nil {
		t.Error("moved below a file")
	}
	put(t, dir, "folder/f.md", "F")
	_, err = v.Move("folder", "other.md")
	if err == nil {
		t.Error("Move accepted a folder")
	}
}

func TestFileCaseOnlyRename(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "Notes.md", "N")
	_, err := v.Move("Notes.md", "notes.md")
	mustOK(t, err)
	names, _ := listDisk(dir)
	if len(names) != 1 || names[0] != "notes.md" || disk(t, dir, "notes.md") != "N" {
		t.Fatalf("directory = %v", names)
	}
	// and in a subfolder, back again
	put(t, dir, "d/Readme.md", "R")
	_, err = v.Move("d/Readme.md", "d/README.md")
	mustOK(t, err)
	names, _ = listDisk(filepath.Join(dir, "d"))
	if len(names) != 1 || names[0] != "README.md" {
		t.Fatalf("directory = %v", names)
	}
}

func TestFolderMoveAndRename(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "src/a.md", "A")
	put(t, dir, "src/sub/b.md", "B")
	put(t, dir, "src/data.json", "{}") // non-md travels along
	_, err := v.FolderMove("src", "dst/renamed")
	mustOK(t, err)
	if exists(dir, "src") || disk(t, dir, "dst/renamed/sub/b.md") != "B" || disk(t, dir, "dst/renamed/data.json") != "{}" {
		t.Fatal("folder not moved intact")
	}
	put(t, dir, "other/x.md", "X")
	_, err = v.FolderMove("other", "dst/renamed")
	wantErr(t, err, "already exists")
	_, err = v.FolderMove("dst", "dst/renamed/inside")
	wantErr(t, err, "into itself")
	_, err = v.FolderMove("dst", "DST/inside")
	wantErr(t, err, "into itself")
	_, err = v.FolderMove("", "x")
	if err == nil {
		t.Error("moved the root")
	}
	_, err = v.FolderMove("other", "")
	if err == nil {
		t.Error("moved onto the root")
	}
	_, err = v.FolderMove("nope", "x")
	wantErr(t, err, "not found")
	_, err = v.FolderMove("other", ".hidden")
	if err == nil {
		t.Error("moved to a hidden name")
	}
	put(t, dir, "file.md", "x")
	_, err = v.FolderMove("file.md", "y")
	if err == nil {
		t.Error("FolderMove accepted a file")
	}
}

func TestFolderCaseOnlyRename(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "Projects/a.md", "A")
	_, err := v.FolderMove("Projects", "projects")
	mustOK(t, err)
	names, _ := listDisk(dir)
	if len(names) != 1 || names[0] != "projects" || disk(t, dir, "projects/a.md") != "A" {
		t.Fatalf("directory = %v", names)
	}
}

func TestFolderCreate(t *testing.T) {
	v, dir, _ := newVault(t)
	p, err := v.FolderCreate("a/b/c")
	mustOK(t, err)
	if p != "a/b/c" {
		t.Errorf("p = %q", p)
	}
	if st, err := os.Stat(filepath.Join(dir, "a/b/c")); err != nil || !st.IsDir() {
		t.Fatal("not created")
	}
	_, err = v.FolderCreate("a/b/c") // idempotent
	mustOK(t, err)
	put(t, dir, "f.md", "x")
	if _, err := v.FolderCreate("f.md"); err == nil {
		t.Error("created a folder over a file")
	}
	if _, err := v.FolderCreate("f.md/sub"); err == nil {
		t.Error("created a folder below a file")
	}
	for _, bad := range []string{"", "/", ".git", "a/.x"} {
		if _, err := v.FolderCreate(bad); err == nil {
			t.Errorf("FolderCreate(%q) succeeded", bad)
		}
	}
}

func TestDeleteGoesToSystemTrash(t *testing.T) {
	v, dir, ft := newVault(t)
	put(t, dir, "d/a.md", "A")
	r, err := v.Delete("d/a.md")
	mustOK(t, err)
	if r.Trash != "system" || len(ft.got) != 1 || ft.got[0] != filepath.Join(dir, "d", "a.md") {
		t.Fatalf("%+v %v", r, ft.got)
	}
	if exists(dir, ".trash") {
		t.Error("fallback folder created although system trash worked")
	}
	_, err = v.Delete("d/a.md")
	wantErr(t, err, "not found")
	_, err = v.Delete("d")
	if err == nil {
		t.Error("Delete accepted a folder")
	}
}

func TestDeleteFallsBackOnlyWhenNoSystemTrash(t *testing.T) {
	v, dir, ft := newVault(t)
	ft.err = errNoTrash
	put(t, dir, "d/a.md", "A")
	r, err := v.Delete("d/a.md")
	mustOK(t, err)
	if r.Trash != "vault" || exists(dir, "d/a.md") || disk(t, dir, ".trash/d/a.md") != "A" {
		t.Fatalf("%+v", r)
	}
	// same name again: nothing is overwritten
	put(t, dir, "d/a.md", "A2")
	_, err = v.Delete("d/a.md")
	mustOK(t, err)
	put(t, dir, "d/a.md", "A3")
	_, err = v.Delete("d/a.md")
	mustOK(t, err)
	names, _ := listDisk(filepath.Join(dir, ".trash", "d"))
	if len(names) != 3 {
		t.Fatalf("trash = %v", names)
	}
	// the fallback folder stays invisible to every tool
	l, _ := v.List("", All)
	for _, e := range l {
		if strings.HasPrefix(e.Name, ".") {
			t.Errorf("hidden entry listed: %v", e)
		}
	}
	if _, err := v.Read("s", ".trash/d/a.md", 0, 0); err == nil {
		t.Error("read from .trash")
	}
}

func TestDeleteOtherFailuresDoNotFallBack(t *testing.T) {
	v, dir, ft := newVault(t)
	ft.err = os.ErrPermission
	put(t, dir, "a.md", "A")
	_, err := v.Delete("a.md")
	wantErr(t, err, "trash")
	noLeak(t, dir, err)
	if disk(t, dir, "a.md") != "A" || exists(dir, ".trash") {
		t.Fatal("file moved although the trash failed for a real reason")
	}
}

func TestDeleteRefusesSymlinkSwappedIntoPath(t *testing.T) {
	v, dir, ft := newVault(t)
	put(t, dir, "real/a.md", "A")
	os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link"))
	if _, err := v.Delete("link/a.md"); err == nil {
		t.Error("deleted through a symlink")
	}
	if len(ft.got) != 0 {
		t.Error("trasher was called")
	}
}

func TestFolderDelete(t *testing.T) {
	v, dir, ft := newVault(t)
	put(t, dir, "full/a.md", "A")
	_, err := v.FolderDelete("full")
	wantErr(t, err, "not empty")
	put(t, dir, "hasdir/sub/x.txt", "x")
	_, err = v.FolderDelete("hasdir")
	wantErr(t, err, "not empty")

	// empty as clients see it: hidden and non-markdown leftovers are allowed
	put(t, dir, "ok/data.json", "{}")
	put(t, dir, "ok/.hidden", "h")
	r, err := v.FolderDelete("ok")
	mustOK(t, err)
	if r.Trash != "system" || len(ft.got) != 1 || ft.got[0] != filepath.Join(dir, "ok") {
		t.Fatalf("%+v %v", r, ft.got)
	}
	v.FolderCreate("empty")
	_, err = v.FolderDelete("empty")
	mustOK(t, err)

	for _, bad := range []string{"", "/", "nope"} {
		if _, err := v.FolderDelete(bad); err == nil {
			t.Errorf("FolderDelete(%q) succeeded", bad)
		}
	}
	put(t, dir, "f.md", "x")
	if _, err := v.FolderDelete("f.md"); err == nil {
		t.Error("FolderDelete accepted a file")
	}
}

func TestSuffixed(t *testing.T) {
	cases := [][3]string{
		{".trash/a/b.md", "S", ".trash/a/b.S.md"},
		{".trash/noext", "S", ".trash/noext.S"},
		{".trash/a.b/c", "S", ".trash/a.b/c.S"},
	}
	for _, c := range cases {
		if got := suffixed(c[0], c[1], 1); got != c[2] {
			t.Errorf("suffixed(%q) = %q, want %q", c[0], got, c[2])
		}
	}
}
