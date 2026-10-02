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

## macOS app

`Rhyolite.app` is a native wrapper around the CLI for people who'd rather not use a terminal. It embeds `rhyolite` and runs it; the server itself is unchanged.

```sh
make app                      # -> bin/Rhyolite.app (macOS 14+, needs Xcode or the Command Line Tools)
open bin/Rhyolite.app
```

- **First launch** opens a window where you choose the vault folder.
- **Later launches** open no window and start the server on the saved folder. If the folder is gone, or the server fails to start (for example, the port is taken), the window opens instead.
- The window has a **Start / Stop** button (Start is disabled until the folder exists) and a live log of the CLI's output, including the `mcp.json` snippets with your token. The log is kept in memory only.
- Closing the window keeps the app and server running. Click the Dock icon to reopen it. **Quit** (menu or ⌘Q) stops the server first: it sends the CLI the same signal as Ctrl+C and only forces it after a timeout.

The build is ad-hoc signed, which is enough on the Mac that built it. An app downloaded from elsewhere is quarantined by Gatekeeper: right-click → Open, or run `xattr -dr com.apple.quarantine Rhyolite.app`. Developer ID signing and notarization aren't set up yet (`SIGN_IDENTITY` in `scripts/build.sh` is the hook for it).

## Develop

```sh
make build                    # -> bin/rhyolite
make test                     # gofmt, vet, cross-compile check, tests with -race (+ the app's Swift tests on macOS)
make cover                    # + coverage
make fuzz                     # + 30s fuzzing of path validation
make app                      # bin/Rhyolite.app (macOS only)
make release                  # dist/ binaries for macOS, Linux, Windows (CGO-free), plus Rhyolite-macos.zip on macOS
```

`make` wraps the scripts in `scripts/`, which take extra flags (e.g. `scripts/test.sh --real-trash` also tests against the real OS trash).

Logs go to `rhyolite.log` in the vault (rolled monthly to `rhyolite.YYYYMM.log`); the token and file contents are never logged.
