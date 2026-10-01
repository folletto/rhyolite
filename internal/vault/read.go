package vault

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"unicode/utf8"
)

// Limits for file_read, matching Claude Code's Read tool.
const (
	ReadMaxLines    = 2000
	ReadMaxLineLen  = 2000
	ReadMaxBytes    = 256 << 10 // whole-file reads above this need offset/limit
	maxKeptLineByte = 64 << 10
)

// ReadResult is the outcome of Read.
type ReadResult struct {
	Path             string         `json:"path"`
	Frontmatter      map[string]any `json:"frontmatter"`
	FrontmatterError string         `json:"frontmatter_error,omitempty"`
	Content          string         `json:"content"`
	Offset           int            `json:"offset"`
	LinesReturned    int            `json:"lines_returned"`
	TotalLines       int            `json:"total_lines"`
	NextOffset       int            `json:"next_offset,omitempty"`
	Truncated        bool           `json:"truncated"`
	LineEnding       string         `json:"line_ending"`
	Size             int64          `json:"size"`
	Modified         string         `json:"modified"`
	Hash             string         `json:"hash"`
}

// MetaResult is the outcome of MetaRead / MetaWrite.
type MetaResult struct {
	Path             string         `json:"path"`
	Frontmatter      map[string]any `json:"frontmatter"`
	FrontmatterError string         `json:"frontmatter_error,omitempty"`
	Size             int64          `json:"size"`
	Modified         string         `json:"modified"`
	Hash             string         `json:"hash"`
	Notice           string         `json:"notice,omitempty"`
}

// lineReader reads lines of bounded memory: only the first keep bytes of each
// line are retained, but the full length is reported.
type lineReader struct {
	r    *bufio.Reader
	keep int
}

func (l *lineReader) next() (line string, crlf bool, err error) {
	var kept []byte
	total := 0
	for {
		chunk, e := l.r.ReadSlice('\n')
		total += len(chunk)
		if room := l.keep - len(kept); room > 0 {
			if len(chunk) > room {
				kept = append(kept, chunk[:room]...)
			} else {
				kept = append(kept, chunk...)
			}
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		err = e
		break
	}
	if total == 0 && err != nil {
		return "", false, err
	}
	s := string(kept)
	if total > l.keep { // cut on a rune boundary
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	if strings.HasSuffix(s, "\n") {
		s = s[:len(s)-1]
		if strings.HasSuffix(s, "\r") {
			s, crlf = s[:len(s)-1], true
		}
	}
	return s, crlf, nil
}

func truncateLine(s string) (string, bool) {
	if len(s) <= ReadMaxLineLen {
		return s, false
	}
	cut := ReadMaxLineLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + " …[line truncated]", true
}

// Read returns a range of lines of a file plus its frontmatter. offset is
// 1-based; limit 0 means "as many as allowed". Without offset/limit, files
// over 256 KB are refused so a whole big file never floods the client.
func (v *Vault) Read(session, rel string, offset, limit int) (*ReadResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	res, err := v.read(session, rel, offset, limit)
	return res, v.sanitize(rel, err)
}

func (v *Vault) read(session, rel string, offset, limit int) (*ReadResult, error) {
	ranged := offset > 0 || limit > 0
	if offset < 1 {
		offset = 1
	}
	if limit < 1 || limit > ReadMaxLines {
		limit = ReadMaxLines
	}
	info, err := v.existingFile(rel)
	if err != nil {
		return nil, err
	}
	if !ranged && info.Size() > ReadMaxBytes {
		return nil, errf("file is %d KB, above the %d KB limit for a whole-file read; read it in ranges with offset and limit (up to %d lines per call)", info.Size()>>10, ReadMaxBytes>>10, ReadMaxLines)
	}
	f, err := v.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	lr := &lineReader{r: bufio.NewReaderSize(io.TeeReader(f, h), 64<<10), keep: maxKeptLineByte}

	res := &ReadResult{Path: rel, Offset: offset, LineEnding: "lf", Size: info.Size(), Modified: rfc3339(info.ModTime())}
	var fm fmCollector
	var out strings.Builder
	lineNo, outBytes := 0, 0
	anyCut := false
	for {
		line, crlf, err := lr.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		lineNo++
		if lineNo == 1 {
			line = strings.TrimPrefix(line, bom)
		}
		if !utf8.ValidString(line) {
			return nil, errf("file is not valid UTF-8")
		}
		if crlf {
			res.LineEnding = "crlf"
		}
		if lineNo <= 1 || fm.state == 1 {
			fm.feed(line)
		}
		if lineNo >= offset && res.LinesReturned < limit && !res.Truncated {
			text, cut := truncateLine(line)
			anyCut = anyCut || cut
			if outBytes+len(text)+1 > ReadMaxBytes && res.LinesReturned > 0 {
				res.Truncated = true
				res.NextOffset = lineNo
			} else {
				out.WriteString(text)
				out.WriteByte('\n')
				outBytes += len(text) + 1
				res.LinesReturned++
			}
		} else if lineNo >= offset && !res.Truncated && res.NextOffset == 0 {
			res.Truncated = true
			res.NextOffset = lineNo
		}
	}
	res.TotalLines = lineNo
	res.Content = out.String()
	res.Hash = hex.EncodeToString(h.Sum(nil))[:16]
	if raw, ok := fm.result(); ok {
		if m, err := parseFrontmatter(raw); err != nil {
			res.FrontmatterError = err.Error()
		} else {
			res.Frontmatter = m
		}
	}
	// Not the whole file in one piece -> a later overwrite must warn.
	partial := res.Truncated || anyCut || offset > 1
	v.trk.set(session, rel, seen{hash: res.Hash, partial: partial})
	return res, nil
}

// MetaRead returns only the frontmatter. It reads the file once to hash it,
// but keeps just the frontmatter in memory.
func (v *Vault) MetaRead(session, rel string) (*MetaResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	res, err := v.metaRead(session, rel)
	return res, v.sanitize(rel, err)
}

func (v *Vault) metaRead(session, rel string) (*MetaResult, error) {
	info, err := v.existingFile(rel)
	if err != nil {
		return nil, err
	}
	f, err := v.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	lr := &lineReader{r: bufio.NewReaderSize(io.TeeReader(f, h), 64<<10), keep: maxKeptLineByte}
	var fm fmCollector
	for n := 0; ; n++ {
		line, _, err := lr.next()
		if err != nil {
			break
		}
		if n == 0 {
			line = strings.TrimPrefix(line, bom)
		}
		if fm.feed(line) {
			break
		}
	}
	io.Copy(io.Discard, lr.r) // finish the hash
	res := &MetaResult{Path: rel, Size: info.Size(), Modified: rfc3339(info.ModTime()), Hash: hex.EncodeToString(h.Sum(nil))[:16]}
	if raw, ok := fm.result(); ok {
		if m, err := parseFrontmatter(raw); err != nil {
			res.FrontmatterError = err.Error()
		} else {
			res.Frontmatter = m
		}
	}
	v.trk.set(session, rel, seen{hash: res.Hash, partial: true})
	return res, nil
}

// frontmatterOf parses the frontmatter of in-memory text (used by search).
func frontmatterOf(text string) map[string]any {
	raw, _, ok := splitFrontmatter(strings.ReplaceAll(strings.TrimPrefix(text, bom), "\r\n", "\n"))
	if !ok {
		return nil
	}
	m, err := parseFrontmatter(raw)
	if err != nil {
		return nil
	}
	return m
}
