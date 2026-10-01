package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/folletto/rhyolite/internal/vault"
)

const instructions = `Rhyolite gives access to a vault: a folder of Markdown (.md) files and subfolders.

- Every path is relative to the vault root, uses "/" separators and never starts with "/". Use "" or "/" for the root itself. Absolute paths and ".." are rejected.
- Only .md files and folders exist for you. Other files and names starting with "." are invisible.
- Files may start with YAML frontmatter between "---" lines. file_read returns it parsed; file_meta_read / file_meta_write read and edit just the frontmatter.
- file_write creates a file and fails if it exists (unless overwrite=true); parent folders are created automatically. For changes to an existing file prefer file_patch (exact old_string -> new_string, like a text editor's find/replace) or file_append.
- file_read returns at most 2000 lines per call; for longer files continue with offset=next_offset. Line endings are preserved on write.
- If a result carries a "notice", the file changed since you last read it: re-read before making edits that depend on surrounding text. Optionally pass expected_hash (the hash from file_read) to make an edit fail if the file changed at all.
- Deleting moves items to the system trash; nothing is destroyed permanently. file_move and folder_move also rename.`

func ptr[T any](v T) *T { return &v }

// Annotation presets.
var (
	readOnly    = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	additive    = &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}
	destructive = &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
)

// add registers a tool whose handler gets the MCP session id (used for the
// per-session staleness tracking) and returns plain Go values; errors become
// tool errors the model can read.
func add[In, Out any](s *mcp.Server, name, desc string, ann *mcp.ToolAnnotations, fn func(session string, in In) (Out, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc, Annotations: ann},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			out, err := fn(req.Session.ID(), in)
			return nil, out, err
		})
}

type listIn struct {
	Path string `json:"path,omitempty" jsonschema:"folder to list, relative to the vault root; empty for the root"`
	Type string `json:"type,omitempty" jsonschema:"all (default), files or folders"`
}
type listOut struct {
	Path  string        `json:"path"`
	Items []vault.Entry `json:"items"`
}

type listRecIn struct {
	Path     string `json:"path,omitempty" jsonschema:"folder to start from; empty for the root"`
	Type     string `json:"type,omitempty" jsonschema:"all (default), files or folders"`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"how many levels to descend; 1 = direct children only; default unlimited"`
	Limit    int    `json:"limit,omitempty" jsonschema:"items per page, default 200, max 1000"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

type readIn struct {
	Path   string `json:"path" jsonschema:"file path relative to the vault root, ending in .md"`
	Offset int    `json:"offset,omitempty" jsonschema:"first line to return, 1-based; use next_offset from a previous call to continue"`
	Limit  int    `json:"limit,omitempty" jsonschema:"number of lines to return, max 2000"`
}

type pathIn struct {
	Path string `json:"path" jsonschema:"path relative to the vault root"`
}

type metaWriteIn struct {
	Path         string         `json:"path" jsonschema:"file path relative to the vault root, ending in .md"`
	Fields       map[string]any `json:"fields" jsonschema:"frontmatter keys to set, e.g. {\"status\":\"done\",\"tags\":[\"a\",\"b\"]}. A key whose value is \"\" or null is deleted. Several keys can be changed in one call."`
	ExpectedHash string         `json:"expected_hash,omitempty" jsonschema:"optional hash from file_read; the call fails if the file changed since"`
}

type writeIn struct {
	Path         string `json:"path" jsonschema:"file path relative to the vault root, ending in .md; missing folders are created"`
	Content      string `json:"content" jsonschema:"full file content, UTF-8"`
	Overwrite    bool   `json:"overwrite,omitempty" jsonschema:"set true to replace an existing file; otherwise an existing file is an error"`
	ExpectedHash string `json:"expected_hash,omitempty" jsonschema:"optional hash from file_read; with overwrite the call fails if the file changed since"`
}

type appendIn struct {
	Path    string `json:"path" jsonschema:"existing file, ending in .md"`
	Content string `json:"content" jsonschema:"text to add at the end; a newline is inserted first if the file does not end with one"`
}

type patchIn struct {
	Path         string `json:"path" jsonschema:"existing file, ending in .md"`
	OldString    string `json:"old_string" jsonschema:"exact text to replace; must match the file exactly (whitespace included) and be unique unless replace_all is set"`
	NewString    string `json:"new_string" jsonschema:"replacement text; empty deletes old_string"`
	ReplaceAll   bool   `json:"replace_all,omitempty" jsonschema:"replace every occurrence of old_string"`
	ExpectedHash string `json:"expected_hash,omitempty" jsonschema:"optional hash from file_read; the call fails if the file changed since"`
}

type moveIn struct {
	Path        string `json:"path" jsonschema:"current path"`
	Destination string `json:"destination" jsonschema:"new full path (this is also how to rename); missing folders are created; fails if it already exists"`
}

type folderPathOut struct {
	Path string `json:"path"`
}

type searchIn struct {
	Query         string `json:"query" jsonschema:"text to find (or a regular expression when regex is true)"`
	Mode          string `json:"mode,omitempty" jsonschema:"all (default): names and content; name: file/folder names only; content: file content only"`
	Regex         bool   `json:"regex,omitempty" jsonschema:"treat query as a regular expression (RE2 syntax)"`
	CaseSensitive bool   `json:"case_sensitive,omitempty" jsonschema:"default false"`
	Path          string `json:"path,omitempty" jsonschema:"only search below this folder"`
	Limit         int    `json:"limit,omitempty" jsonschema:"results per page, default 50, max 200"`
	Cursor        string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func registerTools(s *mcp.Server, v *vault.Vault) {
	add(s, "list", "List the files and subfolders directly inside a folder. Returns name, path, type (file/folder), size in bytes and last modified time.", readOnly,
		func(_ string, in listIn) (listOut, error) {
			items, err := v.List(in.Path, vault.Filter(in.Type))
			return listOut{Path: in.Path, Items: items}, err
		})

	add(s, "list_recursive", "List files and folders at all levels below a folder, in stable tree order, one page at a time. If has_more is true, call again with the returned next_cursor.", readOnly,
		func(_ string, in listRecIn) (*vault.RecursiveResult, error) {
			return v.ListRecursive(in.Path, vault.Filter(in.Type), in.MaxDepth, in.Limit, in.Cursor)
		})

	add(s, "file_read", "Read a Markdown file: its parsed frontmatter plus the content (all lines, including the frontmatter block). Returns at most 2000 lines; if truncated is true, call again with offset=next_offset. Files over 256 KB must be read with offset/limit. Also returns total_lines and a hash of the whole file.", readOnly,
		func(sess string, in readIn) (*vault.ReadResult, error) {
			return v.Read(sess, in.Path, in.Offset, in.Limit)
		})

	add(s, "file_meta_read", "Read only the frontmatter of a Markdown file (much cheaper than file_read).", readOnly,
		func(sess string, in pathIn) (*vault.MetaResult, error) { return v.MetaRead(sess, in.Path) })

	add(s, "file_meta_write", "Set, change or delete frontmatter fields of a Markdown file without touching the body. Pass several keys in 'fields' to change them in one call; a key with value \"\" or null is removed. Comments and key order are preserved; a frontmatter block is created if the file has none.", destructive,
		func(sess string, in metaWriteIn) (*vault.MetaResult, error) {
			return v.MetaWrite(sess, in.Path, in.Fields, in.ExpectedHash)
		})

	add(s, "file_write", "Create a Markdown file with the given content; missing parent folders are created. Fails if the file already exists, unless overwrite=true (which replaces the whole file; prefer file_patch for edits).", destructive,
		func(sess string, in writeIn) (*vault.WriteResult, error) {
			return v.Write(sess, in.Path, in.Content, in.Overwrite, in.ExpectedHash)
		})

	add(s, "file_append", "Append text to the end of an existing Markdown file.", additive,
		func(sess string, in appendIn) (*vault.WriteResult, error) { return v.Append(sess, in.Path, in.Content) })

	add(s, "file_patch", "Edit a file by replacing exact text: old_string must appear in the file exactly once (add surrounding context to make it unique) unless replace_all is true. Works on the file's current content.", destructive,
		func(sess string, in patchIn) (*vault.WriteResult, error) {
			return v.Patch(sess, in.Path, in.OldString, in.NewString, in.ReplaceAll, in.ExpectedHash)
		})

	add(s, "file_move", "Move a file to a new path, which also renames it. Missing destination folders are created; fails if the destination exists.", destructive,
		func(_ string, in moveIn) (*vault.MoveResult, error) { return v.Move(in.Path, in.Destination) })

	add(s, "file_delete", "Delete a file by moving it to the system trash (recoverable).", destructive,
		func(_ string, in pathIn) (*vault.DeleteResult, error) { return v.Delete(in.Path) })

	add(s, "folder_create", "Create a folder, including any missing parents. Succeeds if it already exists.", additive,
		func(_ string, in pathIn) (folderPathOut, error) {
			p, err := v.FolderCreate(in.Path)
			return folderPathOut{Path: p}, err
		})

	add(s, "folder_move", "Move a folder to a new path, which also renames it. Missing destination parents are created; fails if the destination exists or is inside the folder itself.", destructive,
		func(_ string, in moveIn) (*vault.MoveResult, error) { return v.FolderMove(in.Path, in.Destination) })

	add(s, "folder_delete", "Delete a folder, only if it is empty (no .md files and no subfolders), by moving it to the system trash.", destructive,
		func(_ string, in pathIn) (*vault.DeleteResult, error) { return v.FolderDelete(in.Path) })

	add(s, "search", "Search the vault by file/folder name and/or file content. Case-insensitive by default; regex optional. Returns matching paths with their frontmatter and, for content matches, up to 3 snippets with line numbers. Paginated: if has_more is true, call again with next_cursor.", readOnly,
		func(_ string, in searchIn) (*vault.SearchResult, error) {
			return v.Search(vault.SearchOptions{Query: in.Query, Mode: in.Mode, Regex: in.Regex, CaseSensitive: in.CaseSensitive, Path: in.Path, Limit: in.Limit, Cursor: in.Cursor})
		})
}
