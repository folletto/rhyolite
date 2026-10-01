package vault

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func searchTree(t *testing.T, dir string) {
	put(t, dir, "todo.md", "---\ntitle: Todo list\ntags: [work]\n---\nbuy milk\nCall Alice\n")
	put(t, dir, "projects/alpha.md", "# Alpha\nThe quick brown fox\nsecond line with MILK\n")
	put(t, dir, "projects/beta/notes.md", "nothing here\n")
	put(t, dir, "projects/todo-archive/old.md", "milk 1\nmilk 2\nmilk 3\nmilk 4\nmilk 5\n")
	put(t, dir, "Milkshake/recipe.md", "blend\n")
	put(t, dir, "plain.txt", "milk in a txt file")
}

func hitPaths(r *SearchResult) []string {
	out := []string{}
	for _, h := range r.Results {
		out = append(out, h.Path)
	}
	return out
}

func TestSearchModes(t *testing.T) {
	v, dir, _ := newVault(t)
	searchTree(t, dir)

	r, err := v.Search(SearchOptions{Query: "milk"})
	mustOK(t, err)
	want := []string{"Milkshake", "projects/alpha.md", "projects/todo-archive/old.md", "todo.md"}
	if !reflect.DeepEqual(hitPaths(r), want) {
		t.Fatalf("all = %v", hitPaths(r))
	}
	byPath := map[string]SearchHit{}
	for _, h := range r.Results {
		byPath[h.Path] = h
	}
	if byPath["Milkshake"].Match != "name" || byPath["Milkshake"].Type != "folder" {
		t.Errorf("%+v", byPath["Milkshake"])
	}
	if h := byPath["todo.md"]; h.Match != "content" || h.Frontmatter["title"] != "Todo list" || len(h.Snippets) != 1 || h.Snippets[0].Line != 5 || h.Snippets[0].Text != "buy milk" {
		t.Errorf("%+v", h)
	}
	if h := byPath["projects/alpha.md"]; len(h.Snippets) != 1 || h.Snippets[0].Line != 3 {
		t.Errorf("case-insensitive match: %+v", h)
	}
	if h := byPath["projects/todo-archive/old.md"]; len(h.Snippets) != maxSnippets {
		t.Errorf("snippets not capped: %+v", h)
	}

	r, _ = v.Search(SearchOptions{Query: "milk", Mode: ModeName})
	if !reflect.DeepEqual(hitPaths(r), []string{"Milkshake"}) {
		t.Errorf("name = %v", hitPaths(r))
	}
	r, _ = v.Search(SearchOptions{Query: "todo", Mode: ModeName})
	if !reflect.DeepEqual(hitPaths(r), []string{"projects/todo-archive", "todo.md"}) {
		t.Errorf("name todo = %v", hitPaths(r))
	}
	r, _ = v.Search(SearchOptions{Query: "milk", Mode: ModeContent})
	if !reflect.DeepEqual(hitPaths(r), []string{"projects/alpha.md", "projects/todo-archive/old.md", "todo.md"}) {
		t.Errorf("content = %v", hitPaths(r))
	}
	// both
	put(t, dir, "milk.md", "I like milk")
	r, _ = v.Search(SearchOptions{Query: "milk"})
	for _, h := range r.Results {
		if h.Path == "milk.md" && h.Match != "name+content" {
			t.Errorf("%+v", h)
		}
	}
}

func TestSearchCaseSensitivityAndRegex(t *testing.T) {
	v, dir, _ := newVault(t)
	searchTree(t, dir)
	r, _ := v.Search(SearchOptions{Query: "MILK", Mode: ModeContent, CaseSensitive: true})
	if !reflect.DeepEqual(hitPaths(r), []string{"projects/alpha.md"}) {
		t.Errorf("case sensitive = %v", hitPaths(r))
	}
	r, _ = v.Search(SearchOptions{Query: `^call \w+$`, Regex: true, Mode: ModeContent})
	if !reflect.DeepEqual(hitPaths(r), []string{"todo.md"}) {
		t.Errorf("regex (insensitive) = %v", hitPaths(r))
	}
	r, _ = v.Search(SearchOptions{Query: `^call \w+$`, Regex: true, Mode: ModeContent, CaseSensitive: true})
	if len(r.Results) != 0 {
		t.Errorf("regex (sensitive) = %v", hitPaths(r))
	}
	r, _ = v.Search(SearchOptions{Query: `^to.o`, Regex: true, Mode: ModeName})
	if !reflect.DeepEqual(hitPaths(r), []string{"projects/todo-archive", "todo.md"}) {
		t.Errorf("regex name = %v", hitPaths(r))
	}
	_, err := v.Search(SearchOptions{Query: "(", Regex: true})
	wantErr(t, err, "regular expression")
	// regex metacharacters are literal when regex is off
	put(t, dir, "re.md", "a.b (c)\n")
	r, _ = v.Search(SearchOptions{Query: "a.b (c)", Mode: ModeContent})
	if !reflect.DeepEqual(hitPaths(r), []string{"re.md"}) {
		t.Errorf("literal = %v", hitPaths(r))
	}
	// unicode folding
	put(t, dir, "u.md", "CAFÉ au lait\n")
	r, _ = v.Search(SearchOptions{Query: "café", Mode: ModeContent})
	if !reflect.DeepEqual(hitPaths(r), []string{"u.md"}) {
		t.Errorf("unicode = %v", hitPaths(r))
	}
}

func TestSearchScopeAndValidation(t *testing.T) {
	v, dir, _ := newVault(t)
	searchTree(t, dir)
	r, _ := v.Search(SearchOptions{Query: "milk", Path: "projects"})
	if !reflect.DeepEqual(hitPaths(r), []string{"projects/alpha.md", "projects/todo-archive/old.md"}) {
		t.Errorf("scoped = %v", hitPaths(r))
	}
	for _, o := range []SearchOptions{{Query: ""}, {Query: "x", Mode: "bogus"}, {Query: "x", Path: "../.."}, {Query: "x", Path: "nope"}, {Query: "x", Cursor: "!!"}} {
		if _, err := v.Search(o); err == nil {
			t.Errorf("accepted %+v", o)
		}
	}
}

func TestSearchPagination(t *testing.T) {
	v, dir, _ := newVault(t)
	for i := 0; i < 150; i++ {
		put(t, dir, fmt.Sprintf("d%d/n%03d.md", i%3, i), "---\nid: "+fmt.Sprint(i)+"\n---\nneedle\n")
		put(t, dir, fmt.Sprintf("d%d/other%03d.md", i%3, i), "haystack\n")
	}
	all, _ := v.Search(SearchOptions{Query: "needle", Limit: MaxSearchLimit})
	if len(all.Results) != 150 || all.HasMore {
		t.Fatalf("%d results", len(all.Results))
	}
	for _, limit := range []int{1, 7, 50, 149, 150} {
		var got []string
		cur := ""
		for i := 0; i < 400; i++ {
			r, err := v.Search(SearchOptions{Query: "needle", Limit: limit, Cursor: cur})
			mustOK(t, err)
			got = append(got, hitPaths(r)...)
			if !r.HasMore {
				break
			}
			cur = r.NextCursor
		}
		if !reflect.DeepEqual(got, hitPaths(all)) {
			t.Errorf("limit %d: %d results, mismatch", limit, len(got))
		}
	}
	if all.Results[0].Frontmatter["id"] == nil {
		t.Error("frontmatter missing from hit")
	}
}

func TestSearchSkipsBinaryAndHugeFiles(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "bin.md", "needle\x00\x01\x02")
	put(t, dir, "huge.md", "needle "+strings.Repeat("x", maxSearchFileBytes))
	put(t, dir, "ok.md", "needle")
	r, _ := v.Search(SearchOptions{Query: "needle", Mode: ModeContent})
	if !reflect.DeepEqual(hitPaths(r), []string{"ok.md"}) {
		t.Errorf("got %v", hitPaths(r))
	}
}

func TestSearchLongLineSnippetIsClipped(t *testing.T) {
	v, dir, _ := newVault(t)
	put(t, dir, "long.md", strings.Repeat("é", 1000)+" NEEDLE "+strings.Repeat("é", 1000)+"\n")
	r, _ := v.Search(SearchOptions{Query: "needle", Mode: ModeContent})
	if len(r.Results) != 1 {
		t.Fatal("no hit")
	}
	s := r.Results[0].Snippets[0].Text
	if !strings.Contains(s, "NEEDLE") || len(s) > snippetWidth+16 {
		t.Errorf("snippet %d bytes", len(s))
	}
}
