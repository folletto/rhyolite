package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 10, 1, 17, 43, 0, 0, time.UTC) }

func TestFreedesktopHomeTrash(t *testing.T) {
	tmp := t.TempDir()
	data := filepath.Join(tmp, "data")
	src := filepath.Join(tmp, "my notes", "a b.md")
	os.MkdirAll(filepath.Dir(src), 0o755)
	os.WriteFile(src, []byte("x"), 0o644)

	f := freedesktop{dataHome: data, uid: 1000, device: func(string) (uint64, error) { return 1, nil }, now: fixedNow}
	if err := f.Trash(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
	if b, err := os.ReadFile(filepath.Join(data, "Trash", "files", "a b.md")); err != nil || string(b) != "x" {
		t.Fatalf("trashed file missing: %v", err)
	}
	info, _ := os.ReadFile(filepath.Join(data, "Trash", "info", "a b.md.trashinfo"))
	s := string(info)
	if !strings.HasPrefix(s, "[Trash Info]\n") || !strings.Contains(s, "Path="+tmp+"/my%20notes/a%20b.md\n") || !strings.Contains(s, "DeletionDate=2026-10-01T17:43:00\n") {
		t.Errorf("trashinfo = %q", s)
	}
}

func TestFreedesktopNameCollisionsAndFolders(t *testing.T) {
	tmp := t.TempDir()
	data := filepath.Join(tmp, "data")
	f := freedesktop{dataHome: data, uid: 1000, device: func(string) (uint64, error) { return 1, nil }, now: fixedNow}
	for i := 0; i < 3; i++ {
		p := filepath.Join(tmp, "d", "same.md")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
		if err := f.Trash(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"same.md", "same.1.md", "same.2.md"} {
		if _, err := os.Stat(filepath.Join(data, "Trash", "files", n)); err != nil {
			t.Errorf("missing %s", n)
		}
	}
	dir := filepath.Join(tmp, "folder")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	if err := f.Trash(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "Trash", "files", "folder", "sub")); err != nil {
		t.Error("folder not moved with contents")
	}
}

// A file on another device than the home trash goes to $topdir/.Trash-$uid;
// when that cannot be created either, the caller gets ErrUnavailable.
func TestFreedesktopOtherVolume(t *testing.T) {
	tmp := t.TempDir()
	vol := filepath.Join(tmp, "vol") // pretend mount point
	src := filepath.Join(vol, "notes", "a.md")
	os.MkdirAll(filepath.Dir(src), 0o755)
	os.WriteFile(src, []byte("x"), 0o644)
	dev := func(p string) (uint64, error) {
		if p == vol || strings.HasPrefix(p, vol+"/") {
			return 2, nil
		}
		return 1, nil
	}
	f := freedesktop{dataHome: filepath.Join(tmp, "data"), uid: 1000, device: dev, now: fixedNow}
	if err := f.Trash(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vol, ".Trash-1000", "files", "a.md")); err != nil {
		t.Fatal("not in per-volume trash:", err)
	}
	info, _ := os.ReadFile(filepath.Join(vol, ".Trash-1000", "info", "a.md.trashinfo"))
	if !strings.Contains(string(info), "Path=notes/a.md\n") {
		t.Errorf("per-volume path must be relative to topdir: %q", info)
	}

	// Mount point is read-only: nothing can be created -> unavailable.
	ro := filepath.Join(tmp, "ro")
	os.MkdirAll(filepath.Join(ro, "x"), 0o755)
	src2 := filepath.Join(ro, "x", "b.md")
	os.WriteFile(src2, nil, 0o644)
	os.Chmod(ro, 0o500)
	defer os.Chmod(ro, 0o700)
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	dev2 := func(p string) (uint64, error) {
		if p == ro || strings.HasPrefix(p, ro+"/") {
			return 3, nil
		}
		return 1, nil
	}
	f.device = dev2
	if err := f.Trash(src2); err != ErrUnavailable {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if _, err := os.Stat(src2); err != nil {
		t.Error("file must stay in place when no trash is available")
	}
}

// Real OS trash; opt-in so developers' trash isn't cluttered by default.
func TestRealTrash(t *testing.T) {
	if os.Getenv("RHYOLITE_TEST_REAL_TRASH") != "1" {
		t.Skip("set RHYOLITE_TEST_REAL_TRASH=1 to run")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "rhyolite-trash-test-"+time.Now().Format("150405.000")+".md")
	os.WriteFile(p, []byte("test"), 0o644)
	err := System().Trash(p)
	if err == ErrUnavailable {
		t.Skip("no system trash for the temp dir's volume")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file still at original location")
	}
}
