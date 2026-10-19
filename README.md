# rhyolite

Shared access storage for data: a single-binary MCP server that exposes a folder of Markdown files (a "vault") to MCP clients over HTTP, so laptops on the same network can read and edit the same notes.

## Quick start

```sh
make build                    # -> bin/rhyolite
bin/rhyolite ~/Notes          # serve ~/Notes (default: the current folder)
```

On start it creates (or reads) `rhyolite.json` in the folder, holding the access token, then prints ready-to-paste `mcp.json` entries:

```
▲ Rhyolite
  vault: /Users/me/Notes
› Creating: rhyolite.json

● Listening
  on: http://Studio.local:6270/mcp
  or: http://192.168.1.20:6270/mcp
…
```

The first URL uses the machine's local (mDNS) name, which keeps working when the router hands out a new IP; the snippets use it too. The name comes from System Settings → General → Sharing → Local hostname on macOS, the computer name on Windows, and the hostname on Linux when Avahi is running; elsewhere only IPs are shown. The LAN IPs are listed as fallbacks, for networks that block mDNS (guest Wi-Fi, networks with client isolation, many office networks). If two machines claim the same name, macOS renames one (`Studio-2.local`) and client configs need updating. To print a different host first, e.g. a name your router publishes or a fixed IP, set `"advertise": "studio.lan"` in `rhyolite.json` (a bare host or IP, no scheme or port).

Two flavours are printed: one using [`mcp-remote`](https://www.npmjs.com/package/mcp-remote) for clients that insist on HTTPS, and one for clients that speak HTTP directly (e.g. `claude mcp add --transport http`).

Options: `--port N` overrides the port from `rhyolite.json` (default `6270`), `--verbosity none|basic|verbose` sets the terminal output (default `verbose`): `none` prints nothing, `basic` prints only the setup info (configuration and startup banner), `verbose` also echoes every log line. `rhyolite.log` is written in all modes. `--version` prints the version.

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

Security note: the server speaks plain HTTP, so on an untrusted network the bearer token can be sniffed. TLS is not implemented yet. Requests arriving over loopback are accepted only with a `Host` of `localhost`, a loopback IP, the machine's local name or the `advertise` host; other names are refused, which blocks DNS-rebinding attacks.

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

Logs go to `rhyolite.log` in the vault (rolled monthly to `rhyolite.YYYYMM.log`) and every line is also printed to the terminal; the token and file contents are never logged.

Each log line is [logfmt](https://brandur.org/logfmt): one event per line, space-separated `key=value` pairs in a fixed order, always starting with `ts=<RFC3339> event=<name>`. A value that is empty or contains a space, `=`, `"` or a control character is double-quoted (Go string syntax, so `\"` and `\n` escapes); everything else is bare.

| `event` | Keys after `ts` and `event` |
| --- | --- |
| `start` | `version`, `vault`, `listen` |
| `stop` | none |
| `http` | `method`, `path`, `status`, `dur_ms`, `ip` |
| `client_new` | `ip`, `auth` (`ok` or `fail`: whether that first request had a valid token) |
| `auth_fail` | `method`, `path`, `ip` |
| `tool` | `name`, `path` (when the tool takes one), `destination` (moves), `result` (`ok` or `error`), `dur_ms`, and `error` (only on failure; truncated to 200 characters) |

`client_new` is logged once, just before the first `http`/`auth_fail` line from a client IP not seen since the server started (the set is kept in memory, so a restart announces everyone again, and it stops growing at 10,000 IPs). In a terminal that line is printed in bold; the log file never contains escape codes.

```
ts=2026-09-30T12:00:00+02:00 event=client_new ip=192.168.1.20 auth=ok
ts=2026-09-30T12:00:00+02:00 event=http method=POST path=/mcp status=200 dur_ms=12 ip=192.168.1.20
ts=2026-09-30T12:00:00+02:00 event=tool name=file_read path="my notes/a.md" result=ok dur_ms=1
```
