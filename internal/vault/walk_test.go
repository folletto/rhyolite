package vault

import (
	"fmt"
	"reflect"
	"testing"
)

func paths(es []Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out
}

func tree(t *testing.T, dir string) {
	put(t, dir, "b.md", "bb")
	put(t, dir, "a.md", "a")
	put(t, dir, "z/one.md", "1")
	put(t, dir, "z/deep/two.md", "22")
	put(t, dir, "z/deep/er/three.md", "333")
	put(t, dir, "y/only.md", "o")
	put(t, dir, "ignored.txt", "x")
}

func TestList(t *testing.T) {
	v, dir, _ := newVault(t)
	tree(t, dir)
	all, err := v.List("", All)
	mustOK(t, err)
	if got := paths(all); !reflect.DeepEqual(got, []string{"a.md", "b.md", "y", "z"}) {
		t.Fatalf("all = %v", got)
	}
	if all[0].Type != "file" || all[0].Size != 1 || all[0].Modified == "" || all[2].Type != "folder" {
		t.Errorf("%+v", all)
	}
	files, _ := v.List("/", Files)
	if got := paths(files); !reflect.DeepEqual(got, []string{"a.md", "b.md"}) {
		t.Errorf("files = %v", got)
	}
	folders, _ := v.List("", Folders)
	if got := paths(folders); !reflect.DeepEqual(got, []string{"y", "z"}) {
		t.Errorf("folders = %v", got)
	}
	sub, _ := v.List("z/deep", "")
	if got := paths(sub); !reflect.DeepEqual(got, []string{"z/deep/er", "z/deep/two.md"}) {
		t.Errorf("sub = %v", got)
	}
	if _, err := v.List("a.md", All); err == nil {
		t.Error("listed a file")
	}
	if _, err := v.List("nope", All); err == nil {
		t.Error("listed a missing folder")
	}
	if _, err := v.List("", "bogus"); err == nil {
		t.Error("accepted bad filter")
	}
	empty, err := v.List("y", Folders)
	mustOK(t, err)
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty listing = %#v", empty)
	}
}

func TestListRecursiveOrderFiltersAndDepth(t *testing.T) {
	v, dir, _ := newVault(t)
	tree(t, dir)
	r, err := v.ListRecursive("", All, 0, 0, "")
	mustOK(t, err)
	want := []string{"a.md", "b.md", "y", "y/only.md", "z", "z/deep", "z/deep/er", "z/deep/er/three.md", "z/deep/two.md", "z/one.md"}
	if !reflect.DeepEqual(paths(r.Items), want) || r.HasMore || r.NextCursor != "" {
		t.Fatalf("got %v", paths(r.Items))
	}
	r, _ = v.ListRecursive("", Files, 0, 0, "")
	if got := paths(r.Items); !reflect.DeepEqual(got, []string{"a.md", "b.md", "y/only.md", "z/deep/er/three.md", "z/deep/two.md", "z/one.md"}) {
		t.Errorf("files = %v", got)
	}
	r, _ = v.ListRecursive("", Folders, 0, 0, "")
	if got := paths(r.Items); !reflect.DeepEqual(got, []string{"y", "z", "z/deep", "z/deep/er"}) {
		t.Errorf("folders = %v", got)
	}
	r, _ = v.ListRecursive("", All, 1, 0, "")
	if got := paths(r.Items); !reflect.DeepEqual(got, []string{"a.md", "b.md", "y", "z"}) {
		t.Errorf("depth 1 = %v", got)
	}
	r, _ = v.ListRecursive("", All, 2, 0, "")
	if got := paths(r.Items); !reflect.DeepEqual(got, []string{"a.md", "b.md", "y", "y/only.md", "z", "z/deep", "z/one.md"}) {
		t.Errorf("depth 2 = %v", got)
	}
	r, _ = v.ListRecursive("z", All, 0, 0, "")
	if got := paths(r.Items); !reflect.DeepEqual(got, []string{"z/deep", "z/deep/er", "z/deep/er/three.md", "z/deep/two.md", "z/one.md"}) {
		t.Errorf("scoped = %v", got)
	}
}

func TestListRecursivePaginationHasNoGapsOrDuplicates(t *testing.T) {
	v, dir, _ := newVault(t)
	tree(t, dir)
	full, _ := v.ListRecursive("", All, 0, 0, "")
	for _, limit := range []int{1, 2, 3, 4, 7, 9, 10, 11} {
		var got []string
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > 50 {
				t.Fatalf("limit %d: too many pages", limit)
			}
			r, err := v.ListRecursive("", All, 0, limit, cursor)
			mustOK(t, err)
			got = append(got, paths(r.Items)...)
			if !r.HasMore {
				if r.NextCursor != "" {
					t.Error("cursor on last page")
				}
				break
			}
			if r.NextCursor == "" || len(r.Items) != limit {
				t.Fatalf("limit %d: bad page %+v", limit, r)
			}
			cursor = r.NextCursor
		}
		if !reflect.DeepEqual(got, paths(full.Items)) {
			t.Errorf("limit %d: got %v", limit, got)
		}
	}
}

func TestListRecursiveCursorStableWhenItemsAreAdded(t *testing.T) {
	v, dir, _ := newVault(t)
	tree(t, dir)
	r1, _ := v.ListRecursive("", All, 0, 3, "")
	put(t, dir, "a-before.md", "new") // sorts before the cursor
	put(t, dir, "zz.md", "new")       // sorts after
	var rest []string
	cur := r1.NextCursor
	for {
		r, err := v.ListRecursive("", All, 0, 3, cur)
		mustOK(t, err)
		rest = append(rest, paths(r.Items)...)
		if !r.HasMore {
			break
		}
		cur = r.NextCursor
	}
	for _, p := range rest {
		if p == "a-before.md" || p == "a.md" {
			t.Errorf("item before cursor returned again: %s", p)
		}
	}
	if rest[len(rest)-1] != "zz.md" {
		t.Errorf("new item after the cursor missed: %v", rest)
	}
}

func TestListRecursiveLimitsAndBadCursor(t *testing.T) {
	v, dir, _ := newVault(t)
	for i := 0; i < MaxListLimit+50; i++ {
		put(t, dir, fmt.Sprintf("n%04d.md", i), "x")
	}
	r, _ := v.ListRecursive("", All, 0, 0, "")
	if len(r.Items) != DefaultListLimit || !r.HasMore {
		t.Errorf("default limit: %d", len(r.Items))
	}
	r, _ = v.ListRecursive("", All, 0, 99999, "")
	if len(r.Items) != MaxListLimit || !r.HasMore {
		t.Errorf("max limit: %d", len(r.Items))
	}
	for _, c := range []string{"!!!", "Li4vZXRj" /* ../etc */, "L2V0Yy9wYXNzd2Q" /* /etc/passwd */} {
		if _, err := v.ListRecursive("", All, 0, 5, c); err == nil {
			t.Errorf("cursor %q accepted", c)
		}
	}
	if _, err := v.ListRecursive("", All, 0, 5, encodeCursor("other/x.md")); err != nil {
		t.Errorf("valid cursor refused: %v", err)
	}
	if _, err := v.ListRecursive("z", All, 0, 5, encodeCursor("a.md")); err == nil {
		t.Error("cursor outside scope accepted")
	}
}
