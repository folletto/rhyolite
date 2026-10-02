package vault

import (
	"os"
	"sort"
	"testing"
)

func listDisk(dir string) ([]string, error) {
	es, err := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, err
}

func chmod(t *testing.T, p string, m os.FileMode) {
	t.Helper()
	if err := os.Chmod(p, m); err != nil {
		t.Fatal(err)
	}
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	i, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return i.Mode().Perm()
}
