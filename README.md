# rhyolite

Shared access storage for data: a single-binary MCP server that exposes a folder of Markdown files (a "vault") to MCP clients over HTTP, so laptops on the same network can read and edit the same notes.

## Quick start

```sh
make build                    # -> bin/rhyolite
bin/rhyolite ~/Notes          # serve ~/Notes (default: the current folder)
```

On start it creates (or reads) `rhyolite.json` in the folder, holding the access token, then prints ready-to-paste `mcp.json` entries:

```
▴ Creating rhyolite.json
▴ Listening on http://192.168.1.20:6270/mcp
…
```

Two flavours are printed: one using [`mcp-remote`](https://www.npmjs.com/package/mcp-remote) for clients that insist on HTTPS, and one for clients that speak HTTP directly (e.g. `claude mcp add --transport http`).

Options: `--port N` overrides the port from `rhyolite.json` (default `6270`), `--version` prints the version.

To rotate the token, empty the `"token"` field in `rhyolite.json` and restart.

## What it exposes

Only `.md` files and folders. Everything else (including `rhyolite.json` and `rhyolite.log`), names starting with `.`, and symlinks are invisible. All paths are relative to the vault root; nothing outside the folder is reachable (access goes through Go's `os.Root`, plus validation, plus a fuzz test).

| Tool | |
|---|---|
| `list`, `list_recursive` | browse (recursive is paginated) |
| `file_read` | content + parsed frontmatter, partial reads with `offset`/`limit` (2000 lines / 256 KB caps like Claude Code's Read) |
| `file_meta_read`, `file_meta_write` | read / batch-edit frontmatter fields (empty value deletes) |
| `file_write` | create (fails if it exists unless `overwrite: true`) |
| `file_append`, `file_patch` | append; exact `old_string` → `new_string` replacement |
| `file_move`, `folder_move` | move **and** rename; case-only renames work |
| `file_delete`, `folder_delete` | go to the **system trash** (`.trash/` in the vault only if the volume has none); folders only if empty |
| `folder_create` | `mkdir -p` |
| `search` | names and/or content, case-insensitive by default, optional regex, snippets, paginated |

Concurrent edits: edits match against the file's current content, and a `notice` tells a client when its copy is stale. `expected_hash` makes any edit strict. Details in [specs/plan-pr-1.md](specs/plan-pr-1.md) §7.

Security note: the server speaks plain HTTP, so on an untrusted network the bearer token can be sniffed. TLS is not implemented yet.

## Develop

```sh
make build                    # -> bin/rhyolite
make test                     # gofmt, vet, cross-compile check, tests with -race
make cover                    # + coverage
make fuzz                     # + 30s fuzzing of path validation
make release                  # dist/ binaries for macOS, Linux, Windows (CGO-free)
```

`make` wraps the scripts in `scripts/`, which take extra flags (e.g. `scripts/test.sh --real-trash` also tests against the real OS trash).

Logs go to `rhyolite.log` in the vault (rolled monthly to `rhyolite.YYYYMM.log`) and every line is also printed to the terminal; the token and file contents are never logged.
