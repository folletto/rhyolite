# Rhyolite — Implementation Plan

A single-binary MCP server, written in Go, that exposes one folder (the *vault*) of Markdown files to MCP clients on the LAN over HTTP.

## 1. Stack

- **Go ≥ 1.25.** File access goes through `os.Root` (`os.OpenRoot`). The OS refuses any path that escapes the root, through `..` or through symlinks. Go 1.25 added `Rename`, `MkdirAll`, `ReadFile`, `WriteFile` and `Lstat` to `os.Root`, which covers every operation.
- **MCP:** the official SDK `github.com/modelcontextprotocol/go-sdk` with its Streamable HTTP handler.
- **YAML:** `gopkg.in/yaml.v3`. The Node API keeps key order and comments when frontmatter is rewritten.
- **OS trash:** `github.com/ebitengine/purego` (macOS, which calls Foundation without cgo) and `golang.org/x/sys/windows` (Windows). See §5.1.
- No other runtime dependencies. The binary stays `CGO_ENABLED=0` on every platform. Regex uses Go's `regexp` (RE2, linear time, so no ReDoS risk).

## 2. Layout

```
cmd/rhyolite/main.go          flags (--version), startup, banner, graceful shutdown
internal/config/              rhyolite.json load/create/repair, token generation
internal/logfile/             rhyolite.log writer with monthly rollover
internal/vault/
  paths.go                    validation + normalization of vault-relative paths
  vault.go                    os.Root wrapper, per-path locks, atomic writes
  frontmatter.go              split/parse/rewrite YAML frontmatter
  read.go                     partial reads (offset/limit, caps)
  patch.go                    old_string/new_string logic
  walk.go                     sorted walk + cursor pagination
  search.go                   parallel name/content search, regex
  readstate.go                per-session read tracking for staleness checks (§7)
internal/trash/               move to the OS trash
  trash_darwin.go             NSFileManager trashItemAtURL via purego
  trash_linux.go              freedesktop.org Trash spec, pure Go
  trash_windows.go            SHFileOperationW with FOF_ALLOWUNDO
  fallback.go                 vault-local .trash/ when the system has no trash
internal/server/              MCP tool registration, auth/origin middleware, logging
scripts/                      build.sh, test.sh (see §9)
Makefile                      thin wrapper over scripts/
```

## 3. Startup

```
rhyolite [folder]          # default: current directory
rhyolite --version
```

1. Resolve the folder to an absolute path, check that it is a directory, and `os.OpenRoot` it.
2. Handle **`rhyolite.json`**:
   - If it is missing, print `▴ Creating rhyolite.json` and write a new file with mode `0600`.
   - If it is present, print `▴ Reading rhyolite.json`. If `token` is missing or empty, generate a new token and rewrite the file, keeping the other fields. This is how to rotate the token: empty the field and restart.
   - Token: 32 bytes from `crypto/rand`, base64url-encoded.
   - Schema:
     ```json
     { "version": 1, "token": "…", "host": "0.0.0.0", "port": 6270 }
     ```
3. **Port 6270** is the default. `6-2-7` spells **M-C-P** on a phone keypad. IANA lists 6270–6299 as unassigned, and the port avoids the MCP Inspector defaults (6274, 6277). `--port` overrides the config value.
4. Start the HTTP server on `host:port`. `/mcp` sits behind middleware that:
   - checks `Authorization: Bearer <token>` with a constant-time compare, returning 401 otherwise;
   - validates the `Origin` header, as the MCP spec requires (protection against DNS rebinding);
   - caps the request body size;
   - writes every request to the log.
5. Detect LAN IPv4 addresses and print **both** client configs for each one.

   **mcp-remote** (for clients that require HTTPS):
   ```json
   { "mcpServers": { "rhyolite": {
       "command": "npx",
       "args": ["-y", "mcp-remote", "http://192.168.1.20:6270/mcp",
                "--allow-http", "--header", "Authorization:${AUTH_HEADER}"],
       "env": { "AUTH_HEADER": "Bearer <token>" } } } }
   ```

   **Native Streamable HTTP**:
   ```json
   { "mcpServers": { "rhyolite": {
       "type": "http",
       "url": "http://192.168.1.20:6270/mcp",
       "headers": { "Authorization": "Bearer <token>" } } } }
   ```
6. Stop gracefully on SIGINT/SIGTERM.

TLS (self-signed) is deferred. Until then the token travels in clear text on the LAN.

## 4. Logging — `rhyolite.log`

- Written in the vault root. Since it is not `.md`, it is invisible to all tools.
- One line per event:
  - startup and shutdown;
  - each tool call: time, client IP, tool, path(s), outcome, duration;
  - authentication failures.
- **The token and file contents are never logged.**
- **Monthly rollover:** before each write, compare the current month with the month of the last entry. When it changes, rename `rhyolite.log` to `rhyolite.{yyyymm}.log` (the month of the old content) and start a fresh file. The clock is injectable for tests.

## 5. Vault rules (apply to every tool)

- **Paths:**
  - All inputs and outputs are vault-relative with `/` separators (`notes/a.md`). `""` or `/` means the root.
  - Absolute paths, `..` segments, NUL bytes, backslashes and Windows volume names are rejected.
  - Validation is `path.Clean` + `filepath.IsLocal`, with `os.Root` as a second barrier.
- **Markdown only:** tools only see, return, create or modify files ending in `.md` (case-insensitive). Every other file is invisible: listings omit it, and direct access returns "not found". This also blocks access to `rhyolite.json` and `rhyolite.log`. As a separate defence, `rhyolite.json` is also blocked explicitly by name.
- **Folders** are listed normally. Entries whose names start with `.` are hidden (`.trash`, `.git`, `.obsidian`, `.Trash-1000`), as are the temporary files used for atomic writes.
- **Symlinks** are skipped: never listed, never followed, never valid as a target. Entries are checked with `Lstat`.
- **Errors** are sanitized so an absolute host path never appears in a response.
- **Encoding:**
  - Content must be valid UTF-8. Writes with invalid UTF-8 are rejected. Reads of invalid files return an error.
  - A UTF-8 BOM is preserved on rewrite and ignored when parsing frontmatter.
  - **Line endings are preserved.** Each file's style (`\n` or `\r\n`) is detected. Content written by patch, append and meta writes is converted to that style. New files use `\n`.
- **Atomic writes:** write to `.rhyolite-tmp-*` in the same folder, fsync, then `Root.Rename`. Writes are serialized per path with a mutex, so concurrent requests can never corrupt a file.
- **No permanent deletion, ever.** Every delete moves the item to the **operating system's trash** (§5.1). Only if the system has no trash for the vault's volume does it go to a `.trash/` folder in the vault root instead. The server never removes a file outright.
- **Case-only renames** (`Notes.md` → `notes.md`) are detected with `os.SameFile` on the source and destination. They are then done in two steps through a temporary name, so they work on case-insensitive filesystems (macOS, Windows).

### 5.1 OS trash

Deleted items go to the trash the user already uses, so they can be restored from the Finder or file manager. On macOS this includes "Put Back". Using the system trash is the **only** operation that reaches outside the vault, and it touches nothing except the trash.

| OS | Mechanism |
|---|---|
| macOS | `-[NSFileManager trashItemAtURL:resultingItemURL:error:]`, called through purego (no cgo). The system picks the right trash for the volume and records "Put Back" information. |
| Linux | The freedesktop.org Trash spec. On the home volume this is `$XDG_DATA_HOME/Trash/{files,info}`. On other volumes it is `$topdir/.Trash-$uid`, which is created if needed. A `.trashinfo` file records the original path and deletion time. Name collisions get a numeric suffix. |
| Windows | `SHFileOperationW` with `FO_DELETE \| FOF_ALLOWUNDO \| FOF_NOCONFIRMATION \| FOF_SILENT`, through `x/sys/windows`. Items go to the Recycle Bin. **Caveat:** on drives without a Recycle Bin (network shares, some removable drives), this call silently deletes permanently. The drive is therefore checked first with `SHQueryRecycleBinW` on the drive root, and the call is made only if a Recycle Bin exists. |

**Safety**
- The path is validated and `Lstat`-checked through `os.Root` as usual.
- Just before the trash call, the absolute path is rebuilt from the root and checked again. `filepath.EvalSymlinks` must still resolve inside the root, and no component may be a symlink. This keeps the window for a path swapped mid-operation as small as possible.
- **No system trash → `.trash/` fallback.** The fallback is used **only** when the system has no trash for the vault's volume:
  - **macOS:** `trashItemAtURL` fails with `NSFeatureUnsupportedError`. This happens on network and other volumes without a trash.
  - **Linux:** neither the home trash (if it is on the same device) nor `$topdir/.Trash`/`$topdir/.Trash-$uid` can be used or created. The server does not copy across devices.
  - **Windows:** `SHQueryRecycleBinW` reports no Recycle Bin for the drive.

  The fallback moves the item to `<vault>/.trash/` and keeps its relative path, e.g. `.trash/notes/a.md`. If that name is already taken, a timestamp is added: `a.20261001-174300.md`. `.trash/` is a dotfolder, so it is hidden from all tools. Each fallback use is logged.
- **Other failures** (permission denied, I/O error, file in use) do not use the fallback. The tool returns an error and the item stays where it was.
- The probe for whether a trash exists runs once per volume at startup and is cached. The startup banner shows which trash is in use: `▴ Deletes go to: system trash` or `▴ Deletes go to: .trash/ (no system trash on this volume)`.
- Delete results include `trash: "system" | "vault"`.
- The trash API sits behind a `Trasher` interface, so vault tests use a fake.

## 6. Tools

| Tool | Params | Behaviour |
|---|---|---|
| `list` | `path`, `type: all\|files\|folders` | Direct children: name, path, type, size, mtime (RFC 3339 UTC). Only `.md` files and folders. |
| `list_recursive` | `path`, `type`, `max_depth?`, `limit` (default 200, max 1000), `cursor?` | Depth-first walk in sorted order. Returns `items`, `next_cursor`, `has_more`. The cursor is an opaque token holding the last returned path, so it stays stable when items are added. |
| `file_read` | `path`, `offset?` (1-based line), `limit?` (lines) | Returns `frontmatter` (object), `content` (lines in the requested range), `total_lines`, `offset`, `limit`, `next_offset?`, `truncated`, `size`, `mtime`, `hash`. Caps in §6.1. |
| `file_meta_read` | `path` | Reads only up to the closing `---`. Returns `frontmatter`, `mtime`, `hash`. |
| `file_meta_write` | `path`, `fields: {key: value, …}`, `expected_hash?` | Staleness rules (§7). Batch update. Each key is set to its value; an empty value (`""` or `null`) **deletes** the key. Order and comments are kept, and a frontmatter block is created if there is none. Returns the new frontmatter and `hash`. |
| `file_write` | `path`, `content`, `overwrite?` (default false), `expected_hash?` | Creates a new `.md` file and creates missing parent folders. If the file exists, it **errors unless `overwrite: true`**. Overwrites follow the staleness rules (§7). Returns `hash`. |
| `file_append` | `path`, `content` | Appends, adding a newline first if the file doesn't end with one. Errors if the file does not exist. |
| `file_patch` | `path`, `old_string`, `new_string`, `replace_all?`, `expected_hash?` | Claude Code Edit rules (§6.2) and staleness rules (§7). Returns `hash`. |
| `file_move` | `path`, `destination` | Moves and/or renames; `destination` is a full `.md` path. Creates missing parent folders. Errors if the destination exists, unless it is a case-only rename. |
| `file_delete` | `path` | Moves the file to the OS trash, or to `.trash/` as a fallback (§5.1). |
| `folder_create` | `path` | Like `mkdir -p`; succeeds if the folder already exists. |
| `folder_move` | `path`, `destination` | Moves and/or renames; `destination` is the full new folder path. Creates missing parents. Errors if the destination exists (case-only renames excepted) or is inside the source. |
| `folder_delete` | `path` | Allowed only if the folder is empty as clients see it (no `.md` files, no subfolders). The folder is moved to the trash (§5.1) with anything hidden inside it, so nothing is destroyed. The vault root cannot be deleted. |
| `search` | `query`, `mode: all\|name\|content`, `regex?` (default false), `case_sensitive?` (default false), `path?` (scope), `limit` (default 50), `cursor?` | Returns `results: [{path, name, frontmatter, match: name\|content, snippets: [{line, text}]}]`, `next_cursor`, `has_more`. See §6.3. |

`folder_rename` was removed because `folder_move` covers renaming.

Every tool carries MCP annotations: `readOnlyHint` on the reads, `destructiveHint` on delete, move and patch, and `idempotentHint` where it applies. The server's `instructions` field explains the path rules, markdown-only scope, staleness rules and trash behaviour to the model.

### 6.1 Read caps (same as Claude Code's Read)

- Without `offset`/`limit`, at most **2000 lines** are returned from line 1.
- Any line longer than **2000 characters** is truncated, and the response is flagged.
- If a file is **> 256 KB** and no range was given, the call returns an error. The error includes `total_lines` and tells the caller to read in ranges.
- `offset`/`limit` allow reading any part of any file. `next_offset` makes paging simple.

### 6.2 `file_patch` (Claude Code Edit semantics)

1. `old_string` must not be empty and must differ from `new_string`.
2. Exact, case-sensitive match. Before matching, `\n` in both strings is converted to the file's line-ending style.
3. 0 matches → error "old_string not found".
4. More than 1 match with `replace_all=false` → error that reports the match count and asks for more surrounding context.
5. `replace_all=true` replaces every occurrence and reports the count.
6. Patching an `.md` file that does not exist is an error. `file_write` is the only way to create files.
7. Matching is always done against the file's **current** content. Staleness is handled as described in §7.

### 6.3 Search

- The walk scans sorted paths in parallel (a worker pool of `GOMAXPROCS` workers) and reorders results so pagination is deterministic. Only `.md` files are content-searched, and symlinks are skipped.
- **Name mode** matches the file or folder name without opening the file. Folders can match too.
- **Content mode** streams each file line by line and returns up to 3 snippets per file, each with its line number. Lines are trimmed around the match.
- **Literal queries** use a case-folded substring match. **Regex queries** are compiled once, with `(?i)` added unless `case_sensitive` is set.
- Results list name matches first, then content matches, each sorted by path. The cursor encodes the position in that sequence.
- Frontmatter for each result is read with the same function as `file_meta_read`.
- No index for now. If large vaults need one later, an fsnotify-backed index can be added behind the same API.

## 7. Write conflicts: match current content (Claude Code ≥ v2.1.208 approach)

**Problem.** Several laptops can edit the same file. Per-path locks and atomic writes protect the *file* from corruption, but not the *edit*. Client A reads a note, client B changes it, and A then writes from its stale copy, so B's change is lost.

**Approach.** This mirrors Claude Code's current Edit/Write behaviour (see the [tools reference](https://code.claude.com/docs/en/tools-reference)): there is **no read-before-write requirement**. Edits are always applied against the file's current content, so the content itself is the conflict check. When the server knows the client's view is stale, it tells the client.

1. **Hashes.**
   - `file_read` and `file_meta_read` return `hash`, the first 16 hex characters of the SHA-256 of the file bytes.
   - Every write returns the new `hash`.
2. **Read tracking per MCP session.** This is advisory, used to produce notices.
   - The server remembers, for each session, the last hash of each file the session read or wrote, keyed by `Mcp-Session-Id`.
   - It also records whether the last read was **partial**: an `offset`/`limit` range, a 2000-line or line-length cap, or `file_meta_read`.
   - Writes refresh the entry. Entries are dropped when the session ends.

**Rules per tool.** All checks happen under the per-path lock, together with the write.

| Situation | `file_patch` | `file_meta_write` | `file_write` with `overwrite: true` |
|---|---|---|---|
| File never read in this session | Allowed | Allowed | Allowed |
| Read, unchanged since | Allowed | Allowed | Allowed, unless the last read was partial (see the next row) |
| Last read was partial | Allowed | Allowed | **Error:** *"File was only partially read. Read the full file before overwriting it."* |
| Changed since this session's last read | Allowed if `old_string` matches the current content exactly and uniquely. The result includes `notice: "file has changed since your last read; re-read before edits that depend on surrounding content"`. | Allowed; keys are applied to the current frontmatter. Same `notice`. | **Error:** *"File has been modified since it was read. Read it again before overwriting it."* A full overwrite would silently discard the other changes. |
| `old_string` not found or ambiguous | Error, as in §6.2. If the file changed since the last read, the error also says so, so the client knows to re-read. | — | — |
| `expected_hash` given and **mismatched** | **Error:** *"File has been modified since it was read."* | Same | Same |

- **`expected_hash`** is optional and acts as a **strict mode**. A client that wants an exact-version guarantee passes the hash it last saw, and any change fails the call. When it is provided, it takes precedence over the session tracking.
- **Not guarded:** `file_append` (appends don't conflict), `file_write` creating a new file, moves and deletes.

## 8. Test suite

All tests use the standard library `testing` package with table-driven cases and `t.TempDir()` vaults. They run with `-race`.

**`internal/vault`**
- **paths:** `..`, `a/../../b`, absolute paths, `\`, `C:`, NUL, empty, `/`, and unicode names. There is also a **fuzz test** (`FuzzValidatePath`), which asserts that no accepted path resolves outside the root.
- **confinement:** symlinks pointing inside and outside the vault, a symlinked folder in the middle of a path, and a symlinked root parent. All of them are skipped or rejected.
- **visibility:** non-`.md` files, `rhyolite.json`, `rhyolite.log` and dotfiles never show up in `list`, `list_recursive` or `search`, and direct reads of them fail.
- **frontmatter:**
  - Parsing: none, empty, valid, malformed, BOM, and `\r\n`.
  - `file_meta_write`: set, update, delete-on-empty, batch, key order and comments preserved, and creating a block where there was none.
- **read:** default caps, line truncation, refusal above 256 KB, `offset`/`limit` paging, and `next_offset`.
- **patch:**
  - Each rule in §6.2.
  - `replace_all`.
  - Matching across `\r\n` files.
- **staleness (§7):** one test per row of the §7 table.
  - Patch and meta writes on unread files succeed.
  - A changed-since-read patch with a unique match succeeds and returns the `notice`. A stale `old_string` fails, and the error mentions the change.
  - Overwrite fails after a partial read, and fails on a file changed since the read.
  - A mismatched `expected_hash` fails every guarded tool.
  - Chained writes work without reading again.
  - Session state is isolated between sessions.
  - Append and new-file writes are never blocked.
- **write/append:** an existing file is refused without `overwrite`, and replaced with it. Also covers creating missing parents, adding the missing trailing newline, keeping line endings, rejecting invalid UTF-8.
- **move:**
  - Files and folders.
  - Creating missing parents.
  - Destination already exists.
  - Moving a folder into itself.
  - **Case-only rename.** This is tested on the host filesystem and also through a simulated case-insensitive check.
- **delete:** the fake `Trasher` receives the correct absolute paths, the non-empty folder rule holds, and the root cannot be deleted. A trasher that fails leaves the item in place and does **not** trigger the fallback. A trasher reporting "no trash available" triggers the `.trash/` fallback, which keeps the relative path, applies the collision suffix, and stays hidden from tools. A symlink swapped into the path before the trash call is rejected.
- **walk/pagination:** stable order, cursor continuation with no duplicates and no gaps, `max_depth`, type filters, and items inserted between pages.
- **search:**
  - Each mode.
  - Case-insensitive by default; regex on and off.
  - Invalid regex returns an error.
  - Snippets and line numbers.
  - Scoped `path`.
  - Pagination.

**Other packages**
- **`internal/trash`:**
  - **Linux** is fully tested in a temp dir: `XDG_DATA_HOME` points to `t.TempDir()`. The checks cover `.trashinfo` contents (original path, URL-encoding, date), collision suffixes, and folders. Volume detection is simulated through an injected device-ID function, to cover the "no usable trash, so use the fallback" case.
  - **macOS and Windows** run against the real trash only when `RHYOLITE_TEST_REAL_TRASH=1` is set. They create a uniquely named temp file, trash it, and check that it has gone from the original location. They are skipped by default so they don't clutter the developer's trash.
- **`internal/config`:**
  - Create path.
  - Read path.
  - Empty token regenerated with the other fields kept.
  - File mode `0600`.
  - Malformed JSON is reported as an error rather than silently overwritten.
- **`internal/logfile`:**
  - Rollover across a month boundary, using an injected clock.
  - Appending to an existing log after a restart.
  - No token in the output.
- **`internal/server`:**
  - Integration tests start the server with `httptest` and drive every tool through the go-sdk **client** over real HTTP.
  - Auth cases: missing token, wrong token, and correct token.
  - Origin check.
  - Body size limit.
  - Concurrent writes from many goroutines leave the file valid.
  - Two client sessions patching the same file at once:
    - Patches to different regions both succeed, and the second one returns the change `notice`.
    - Patches to the same `old_string`: exactly one succeeds, and the other gets "not found".
    - Two `expected_hash` writes: exactly one succeeds.
- **`cmd/rhyolite`:**
  - Banner output (`▴ Creating…` / `▴ Reading…`) and both JSON configs parse as valid JSON.
  - `--version`.

## 9. Build & test scripts

The scripts in `scripts/` are the source of truth (POSIX `sh`, `set -eu`). The `Makefile` wraps them for convenience.

| Command | Does |
|---|---|
| `scripts/build.sh` | `go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" -o bin/rhyolite ./cmd/rhyolite` |
| `scripts/build.sh --all` | Cross-compiles to `dist/` for darwin/arm64, darwin/amd64, linux/amd64, linux/arm64 and windows/amd64 with `CGO_ENABLED=0` |
| `scripts/test.sh` | `go vet ./...`, then `go test -race -count=1 ./...` |
| `scripts/test.sh --cover` | The same, plus `-coverprofile=coverage.out` and a printed summary |
| `scripts/test.sh --real-trash` | Also runs the real OS-trash tests (`RHYOLITE_TEST_REAL_TRASH=1`) |
| `scripts/test.sh --fuzz` | Runs `FuzzValidatePath` for a bounded time (`-fuzztime=30s`) |
| `make build` / `make test` / `make cover` / `make fuzz` / `make release` / `make clean` | Wrappers over the above |

`bin/`, `dist/` and `coverage.out` go in `.gitignore`.

## 10. Phases

1. **Skeleton:**
   - `go.mod`, scripts, Makefile.
   - config and token handling, logfile.
   - Banner output.
   - HTTP server with auth middleware and no tools yet.
   - Test with mcp-remote from a second laptop.
2. **`vault` core:** path validation, `.md`-only visibility, symlink skipping, atomic writes, the `internal/trash` package for all three OSes plus the `.trash/` fallback, and session read tracking. The tests (including fuzzing) come first.
3. **Read tools:** `list`, `list_recursive`, `file_read`, `file_meta_read`, `search`.
4. **Write tools:** `file_write`, `file_append`, `file_patch`, `file_meta_write`, `file_move`, `file_delete`, `folder_create`, `folder_move`, `folder_delete`.
5. **Hardening:**
   - Error sanitizing audit.
   - Concurrency tests.
   - Cross-compiled release build.
   - README with usage.

## 11. Decisions log

- **Port 6270**, which spells MCP on a phone keypad.
- **Markdown only.** Symlinks are skipped, and dotfiles are hidden.
- **Token.** An empty `token` in `rhyolite.json` is regenerated.
- **Moves.** `file_move` and `folder_move` also rename. `folder_rename` was removed.
- **Frontmatter.** `file_meta_read` and `file_meta_write` (batch; an empty value deletes the key).
- **Read caps** match Claude Code's Read; partial reads use `offset`/`limit`.
- **`file_write`** creates the file and errors if it exists, unless `overwrite: true`.
- **Conflicts.** As in Claude Code ≥ v2.1.208: no read required, edits match the current content, and the client gets a notice when the file changed. A full overwrite after a partial or stale read is refused. `expected_hash` is the optional strict mode (§7).
- **Deletion** goes to the OS trash. `.trash/` in the vault is used only when the volume has no system trash. Nothing is ever deleted permanently (§5.1).
- **Search** is case-insensitive by default, with optional regex.
- **TLS** (self-signed) is deferred.
