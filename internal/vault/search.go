package vault

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"
)

// Search modes.
const (
	ModeAll     = "all"
	ModeName    = "name"
	ModeContent = "content"
)

// Limits for search.
const (
	DefaultSearchLimit = 50
	MaxSearchLimit     = 200
	maxSearchFileBytes = 10 << 20
	maxSnippets        = 3
	snippetWidth       = 240
	searchBatch        = 64
)

// SearchOptions are the parameters of Search.
type SearchOptions struct {
	Query         string
	Mode          string
	Regex         bool
	CaseSensitive bool
	Path          string
	Limit         int
	Cursor        string
}

// Snippet is one matching line.
type Snippet struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SearchHit is one matching file or folder.
type SearchHit struct {
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Match       string         `json:"match"` // name, content, or name+content
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Snippets    []Snippet      `json:"snippets,omitempty"`
}

// SearchResult is one page of results, in tree order.
type SearchResult struct {
	Results    []SearchHit `json:"results"`
	NextCursor string      `json:"next_cursor,omitempty"`
	HasMore    bool        `json:"has_more"`
}

type matcher struct {
	re    *regexp.Regexp
	lower string
	fold  bool
}

func newMatcher(o SearchOptions) (*matcher, error) {
	if o.Regex {
		expr := o.Query
		if !o.CaseSensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, errf("invalid regular expression: %v", err)
		}
		return &matcher{re: re}, nil
	}
	if o.CaseSensitive {
		return &matcher{lower: o.Query}, nil
	}
	return &matcher{lower: strings.ToLower(o.Query), fold: true}, nil
}

// find returns the byte offset of the first match in s, or -1.
func (m *matcher) find(s string) int {
	if m.re != nil {
		if loc := m.re.FindStringIndex(s); loc != nil {
			return loc[0]
		}
		return -1
	}
	if m.fold {
		s = strings.ToLower(s)
	}
	return strings.Index(s, m.lower)
}

// Search finds files and folders by name and/or content. Results come in the
// same stable tree order as ListRecursive, so the cursor works like there.
func (v *Vault) Search(o SearchOptions) (*SearchResult, error) {
	scope, err := Clean(o.Path)
	if err != nil {
		return nil, err
	}
	if o.Query == "" {
		return nil, errf("query must not be empty")
	}
	switch o.Mode {
	case "":
		o.Mode = ModeAll
	case ModeAll, ModeName, ModeContent:
	default:
		return nil, errf("mode must be all, name or content")
	}
	if o.Limit <= 0 {
		o.Limit = DefaultSearchLimit
	}
	if o.Limit > MaxSearchLimit {
		o.Limit = MaxSearchLimit
	}
	m, err := newMatcher(o)
	if err != nil {
		return nil, err
	}
	after, err := decodeCursor(o.Cursor, scope)
	if err != nil {
		return nil, err
	}

	type cand struct {
		e fs.DirEntry
		p string
	}
	res := &SearchResult{Results: []SearchHit{}}
	var batch []cand

	// flush evaluates a batch in parallel and appends hits in order.
	// It returns false once the page is full.
	flush := func() bool {
		hits := make([]*SearchHit, len(batch))
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.GOMAXPROCS(0))
		for i, c := range batch {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				hits[i] = v.evaluate(c.e, c.p, o.Mode, m)
			}()
		}
		wg.Wait()
		batch = batch[:0]
		for _, h := range hits {
			if h == nil {
				continue
			}
			if len(res.Results) == o.Limit {
				res.HasMore = true
				return false
			}
			res.Results = append(res.Results, *h)
		}
		return true
	}

	stopped := false
	err = v.walk(scope, after, 0, func(e fs.DirEntry, p string) bool {
		if e.IsDir() && o.Mode == ModeContent {
			return true // folders have no content
		}
		batch = append(batch, cand{e, p})
		if len(batch) >= searchBatch && !flush() {
			stopped = true
			return false
		}
		return true
	})
	if err != nil {
		return nil, v.sanitize(scope, err)
	}
	if !stopped && len(batch) > 0 {
		flush()
	}
	if res.HasMore {
		res.NextCursor = encodeCursor(res.Results[len(res.Results)-1].Path)
	}
	return res, nil
}

func (v *Vault) evaluate(e fs.DirEntry, p, mode string, m *matcher) *SearchHit {
	h := &SearchHit{Path: p, Name: e.Name(), Type: "file"}
	if e.IsDir() {
		h.Type = "folder"
	}
	nameHit := mode != ModeContent && m.find(e.Name()) >= 0
	var contentHit bool
	if !e.IsDir() && mode != ModeName {
		h.Snippets, contentHit = v.searchContent(p, m)
	}
	switch {
	case nameHit && contentHit:
		h.Match = "name+content"
	case nameHit:
		h.Match = "name"
	case contentHit:
		h.Match = "content"
	default:
		return nil
	}
	if !e.IsDir() {
		h.Frontmatter = v.frontmatterAt(p)
	}
	return h
}

func (v *Vault) frontmatterAt(rel string) map[string]any {
	f, err := v.root.Open(rel)
	if err != nil {
		return nil
	}
	defer f.Close()
	lr := &lineReader{r: bufio.NewReaderSize(f, 16<<10), keep: maxKeptLineByte}
	var fm fmCollector
	for n := 0; ; n++ {
		line, _, err := lr.next()
		if err != nil {
			return nil
		}
		if n == 0 {
			line = strings.TrimPrefix(line, bom)
		}
		if fm.feed(line) {
			break
		}
	}
	raw, ok := fm.result()
	if !ok {
		return nil
	}
	m, _ := parseFrontmatter(raw)
	return m
}

// searchContent scans a file line by line. Binary-looking and oversized
// files are skipped.
func (v *Vault) searchContent(rel string, m *matcher) ([]Snippet, bool) {
	info, err := v.lstat(rel)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSearchFileBytes {
		return nil, false
	}
	f, err := v.root.Open(rel)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64<<10)
	if head, _ := br.Peek(8192); bytes.IndexByte(head, 0) >= 0 {
		return nil, false
	}
	lr := &lineReader{r: br, keep: maxKeptLineByte}
	var snips []Snippet
	found := false
	for n := 1; ; n++ {
		line, _, err := lr.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if n == 1 {
			line = strings.TrimPrefix(line, bom)
		}
		if i := m.find(line); i >= 0 {
			found = true
			if len(snips) < maxSnippets {
				snips = append(snips, Snippet{Line: n, Text: clip(line, i)})
			} else {
				break
			}
		}
	}
	return snips, found
}

// clip shortens a line to about snippetWidth bytes around position at.
func clip(s string, at int) string {
	if len(s) <= snippetWidth {
		return s
	}
	start := at - snippetWidth/3
	if start < 0 {
		start = 0
	}
	end := start + snippetWidth
	if end > len(s) {
		end = len(s)
	}
	for start > 0 && !utf8.RuneStart(s[start]) {
		start--
	}
	for end < len(s) && !utf8.RuneStart(s[end]) {
		end++
	}
	out := s[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}
