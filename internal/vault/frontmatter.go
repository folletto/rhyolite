package vault

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	bom           = "\xef\xbb\xbf"
	maxFrontBytes = 256 << 10
)

// fmCollector recognises a YAML frontmatter block line by line: an opening
// "---" on the very first line, closed by "---" or "...".
type fmCollector struct {
	state int // 0 awaiting first line, 1 collecting, 2 complete, 3 none
	buf   strings.Builder
}

// feed takes one line without its terminator and reports whether the
// collector is finished (complete or not frontmatter).
func (c *fmCollector) feed(line string) bool {
	switch c.state {
	case 0:
		if strings.TrimRight(strings.TrimPrefix(line, bom), " \t") == "---" {
			c.state = 1
		} else {
			c.state = 3
		}
	case 1:
		t := strings.TrimRight(line, " \t")
		if t == "---" || t == "..." {
			c.state = 2
		} else if c.buf.Len()+len(line) > maxFrontBytes {
			c.state = 3
		} else {
			c.buf.WriteString(line)
			c.buf.WriteByte('\n')
		}
	}
	return c.state >= 2
}

// end tells the collector the file has ended: an unclosed block isn't frontmatter.
func (c *fmCollector) result() (raw string, found bool) {
	if c.state == 2 {
		return c.buf.String(), true
	}
	return "", false
}

// splitFrontmatter splits LF-normalised text (BOM already removed) into the
// YAML between the delimiters and everything after the closing line.
func splitFrontmatter(text string) (raw, rest string, found bool) {
	var c fmCollector
	pos := 0
	for pos <= len(text) {
		nl := strings.IndexByte(text[pos:], '\n')
		var line string
		next := len(text) + 1
		if nl < 0 {
			line = text[pos:]
		} else {
			line, next = text[pos:pos+nl], pos+nl+1
		}
		if c.feed(line) {
			if r, ok := c.result(); ok {
				if next > len(text) {
					next = len(text)
				}
				return r, text[next:], true
			}
			return "", text, false
		}
		pos = next
	}
	return "", text, false
}

// parseFrontmatter decodes YAML into JSON-safe values.
func parseFrontmatter(raw string) (map[string]any, error) {
	var doc any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return map[string]any{}, nil
	}
	m, ok := jsonSafe(doc).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("frontmatter is not a key/value mapping")
	}
	return m, nil
}

func jsonSafe(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = jsonSafe(x)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[fmt.Sprint(k)] = jsonSafe(x)
		}
		return out
	case []any:
		for i, x := range t {
			t[i] = jsonSafe(x)
		}
		return t
	}
	return v
}

// applyFields sets/deletes top-level keys in the frontmatter of text
// (LF-normalised, BOM removed) and returns the new text. A nil or empty
// string value deletes the key. Key order and comments are preserved.
// changed is false when nothing needed to be written.
func applyFields(text string, fields map[string]any) (out string, changed bool, err error) {
	raw, rest, found := splitFrontmatter(text)

	var doc yaml.Node
	if found {
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return "", false, fmt.Errorf("existing frontmatter is not valid YAML: %v", err)
		}
	}
	var m *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		m = doc.Content[0]
		if m.Kind != yaml.MappingNode {
			if m.Kind == yaml.ScalarNode && m.Tag == "!!null" {
				m = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				doc.Content[0] = m
			} else {
				return "", false, fmt.Errorf("existing frontmatter is not a key/value mapping")
			}
		}
	} else {
		m = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{m}}
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		val := fields[k]
		del := val == nil
		if s, ok := val.(string); ok && s == "" {
			del = true
		}
		idx := -1
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == k {
				idx = i
				break
			}
		}
		if del {
			if idx >= 0 {
				m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
				changed = true
			}
			continue
		}
		var vn yaml.Node
		if err := vn.Encode(val); err != nil {
			return "", false, fmt.Errorf("cannot encode value for %q: %v", k, err)
		}
		if idx >= 0 {
			old := m.Content[idx+1]
			vn.HeadComment, vn.LineComment, vn.FootComment = old.HeadComment, old.LineComment, old.FootComment
			m.Content[idx+1] = &vn
		} else {
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, &vn)
		}
		changed = true
	}
	if !changed {
		return text, false, nil
	}

	body := ""
	if len(m.Content) > 0 {
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(&doc); err != nil {
			return "", false, err
		}
		enc.Close()
		body = buf.String()
	}
	if found {
		return "---\n" + body + "---\n" + rest, true, nil
	}
	return "---\n" + body + "---\n" + text, true, nil
}
