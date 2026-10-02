package vault

import (
	"io/fs"
	"strings"
	"unicode/utf8"
)

// WriteResult is the outcome of a write-type operation.
type WriteResult struct {
	Path         string `json:"path"`
	Hash         string `json:"hash"`
	Size         int    `json:"size"`
	Created      bool   `json:"created,omitempty"`
	Replacements int    `json:"replacements,omitempty"`
	Notice       string `json:"notice,omitempty"`
}

const staleNotice = "File has changed since your last read of it; re-read before making edits that depend on the surrounding content."

type kind int

const (
	kindEdit      kind = iota // patch / meta write: matched against current content
	kindOverwrite             // whole-file replacement
)

// guard applies the staleness rules (specs/plan-pr-1.md §7) to a file that exists, given
// the hash of its current content. It returns a notice for the client, or an error.
func (v *Vault) guard(session, rel, current, expected string, k kind) (notice string, err error) {
	if expected != "" {
		if expected != current {
			return "", errModified
		}
		return "", nil
	}
	e, ok := v.trk.get(session, rel)
	if !ok {
		return "", nil
	}
	switch k {
	case kindOverwrite:
		if e.partial {
			return "", errPartial
		}
		if e.hash != current {
			return "", errModified
		}
	case kindEdit:
		if e.hash != current {
			return staleNotice, nil
		}
	}
	return "", nil
}

// afterEdit moves a session's record of a file along with an edit it made,
// but only if the session's view was current: an edit is not a read, so it
// must not turn a stale or partial view into a full one.
func (v *Vault) afterEdit(session, rel, before, after string) {
	if e, ok := v.trk.get(session, rel); ok && e.hash == before {
		v.trk.set(session, rel, seen{hash: after, partial: e.partial})
	}
}

func checkText(s string) error {
	if !utf8.ValidString(s) {
		return errf("content is not valid UTF-8")
	}
	if strings.ContainsRune(s, 0) {
		return errf("content contains NUL bytes")
	}
	return nil
}

// fileStyle splits a file into BOM, line-ending style, and LF-normalised text.
func fileStyle(b []byte) (prefix string, crlf bool, text string) {
	s := string(b)
	if strings.HasPrefix(s, bom) {
		prefix, s = bom, s[len(bom):]
	}
	if strings.Contains(s, "\r\n") {
		crlf = true
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	return prefix, crlf, s
}

func joinStyle(prefix string, crlf bool, text string) []byte {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(prefix + text)
}

// Write creates a new .md file (and missing folders). An existing file is an
// error unless overwrite is set.
func (v *Vault) Write(session, rel, content string, overwrite bool, expectedHash string) (*WriteResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	if err := checkText(content); err != nil {
		return nil, err
	}
	if len(content) > MaxFileBytes {
		return nil, errf("content is larger than %d MB", MaxFileBytes>>20)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	res, err := v.write(session, rel, content, overwrite, expectedHash)
	return res, v.sanitize(rel, err)
}

func (v *Vault) write(session, rel, content string, overwrite bool, expectedHash string) (*WriteResult, error) {
	if err := v.checkParents(rel); err != nil {
		return nil, err
	}
	res := &WriteResult{Path: rel, Size: len(content)}
	perm := fs.FileMode(0o644)
	info, err := v.lstat(rel)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return nil, errf("%q exists and is not a file", rel)
		}
		if !overwrite {
			return nil, errf("%q already exists; pass overwrite=true to replace it", rel)
		}
		old, _, err := v.readAll(rel)
		if err != nil {
			return nil, err
		}
		if res.Notice, err = v.guard(session, rel, hashBytes(old), expectedHash, kindOverwrite); err != nil {
			return nil, err
		}
		perm = info.Mode().Perm()
	case isNotExist(err):
		res.Created = true
		if err := v.mkParents(rel); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if err := v.writeAtomic(rel, []byte(content), perm); err != nil {
		return nil, err
	}
	res.Hash = hashBytes([]byte(content))
	v.trk.set(session, rel, seen{hash: res.Hash})
	return res, nil
}

// Append adds content to the end of an existing file, inserting a newline
// first when the file does not end with one.
func (v *Vault) Append(session, rel, content string) (*WriteResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	if err := checkText(content); err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	res, err := v.appendLocked(session, rel, content)
	return res, v.sanitize(rel, err)
}

func (v *Vault) appendLocked(session, rel, content string) (*WriteResult, error) {
	old, info, err := v.readAll(rel)
	if err != nil {
		return nil, err
	}
	if len(old)+len(content) > MaxFileBytes {
		return nil, errf("file would exceed %d MB", MaxFileBytes>>20)
	}
	prefix, crlf, text := fileStyle(old)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	data := joinStyle(prefix, crlf, text+content)
	if err := v.writeAtomic(rel, data, info.Mode().Perm()); err != nil {
		return nil, err
	}
	h := hashBytes(data)
	v.afterEdit(session, rel, hashBytes(old), h)
	return &WriteResult{Path: rel, Hash: h, Size: len(data)}, nil
}

// Patch replaces old with new in a file, with the semantics of Claude Code's
// Edit tool: exact match against the current content, which must be unique
// unless replaceAll is set.
func (v *Vault) Patch(session, rel, oldStr, newStr string, replaceAll bool, expectedHash string) (*WriteResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	if oldStr == "" {
		return nil, errf("old_string must not be empty")
	}
	if oldStr == newStr {
		return nil, errf("old_string and new_string are identical; nothing to change")
	}
	if err := checkText(newStr); err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	res, err := v.patch(session, rel, oldStr, newStr, replaceAll, expectedHash)
	return res, v.sanitize(rel, err)
}

func (v *Vault) patch(session, rel, oldStr, newStr string, replaceAll bool, expectedHash string) (*WriteResult, error) {
	data, info, err := v.readAll(rel)
	if err != nil {
		return nil, err
	}
	cur := hashBytes(data)
	notice, err := v.guard(session, rel, cur, expectedHash, kindEdit)
	if err != nil {
		return nil, err
	}
	prefix, crlf, text := fileStyle(data)
	// Compare in LF space, so the client's \n matches \r\n files.
	oldStr = strings.ReplaceAll(oldStr, "\r\n", "\n")
	newStr = strings.ReplaceAll(newStr, "\r\n", "\n")
	n := strings.Count(text, oldStr)
	switch {
	case n == 0:
		msg := "old_string was not found in the file"
		if notice != "" {
			msg += ". The file has changed since you last read it: read it again"
		}
		return nil, errf("%s", msg)
	case n > 1 && !replaceAll:
		return nil, errf("old_string appears %d times in the file; add more surrounding context to make it unique, or set replace_all=true", n)
	}
	count := 1
	if replaceAll {
		text, count = strings.ReplaceAll(text, oldStr, newStr), n
	} else {
		text = strings.Replace(text, oldStr, newStr, 1)
	}
	out := joinStyle(prefix, crlf, text)
	if len(out) > MaxFileBytes {
		return nil, errf("file would exceed %d MB", MaxFileBytes>>20)
	}
	if err := v.writeAtomic(rel, out, info.Mode().Perm()); err != nil {
		return nil, err
	}
	h := hashBytes(out)
	v.afterEdit(session, rel, cur, h)
	return &WriteResult{Path: rel, Hash: h, Size: len(out), Replacements: count, Notice: notice}, nil
}

// MetaWrite sets frontmatter fields; a nil or empty-string value deletes the field.
func (v *Vault) MetaWrite(session, rel string, fields map[string]any, expectedHash string) (*MetaResult, error) {
	rel, err := CleanFile(rel)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errf("fields must contain at least one key")
	}
	for k := range fields {
		if k == "" || strings.ContainsAny(k, "\n\r") {
			return nil, errf("invalid field name %q", k)
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	res, err := v.metaWrite(session, rel, fields, expectedHash)
	return res, v.sanitize(rel, err)
}

func (v *Vault) metaWrite(session, rel string, fields map[string]any, expectedHash string) (*MetaResult, error) {
	data, info, err := v.readAll(rel)
	if err != nil {
		return nil, err
	}
	cur := hashBytes(data)
	notice, err := v.guard(session, rel, cur, expectedHash, kindEdit)
	if err != nil {
		return nil, err
	}
	prefix, crlf, text := fileStyle(data)
	newText, changed, err := applyFields(text, fields)
	if err != nil {
		return nil, errf("%v", err)
	}
	out := data
	if changed {
		out = joinStyle(prefix, crlf, newText)
		if err := v.writeAtomic(rel, out, info.Mode().Perm()); err != nil {
			return nil, err
		}
	}
	h := hashBytes(out)
	v.afterEdit(session, rel, cur, h)
	res := &MetaResult{Path: rel, Size: int64(len(out)), Hash: h, Notice: notice}
	if changed {
		res.Modified = rfc3339(v.now())
	} else {
		res.Modified = rfc3339(info.ModTime())
	}
	if m := frontmatterOf(string(out)); m != nil {
		res.Frontmatter = m
	} else {
		res.Frontmatter = map[string]any{}
	}
	return res, nil
}
