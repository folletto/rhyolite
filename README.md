# rhyolite

Shared access storage for data: a single-binary MCP server that exposes a folder of Markdown files (a "vault") to MCP clients over HTTP, so laptops on the same network can read and edit the same notes.

Contains:
* `rhyolite` CLI binary
* Rhyolite MacOS app (UI for the CLI)

<img width="1504" height="1248" alt="rhyolite-20261024" src="https://github.com/user-attachments/assets/a98dc7fd-016a-4f1f-9755-ac51df98e497" />



## Why

The idea is to have an always-on MCP that can be accessed locally (or via LAN) that gives access to a folder. The MCP tools exposed are as close as possible to the same tools that LLMs use to read/write files, aiming to be analogous as navigating a file system. 

This can also be pointed to an Obsidian folder, allowing live edit of the same set of files.

No tracking, no analytics. It's all in your control.


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
› Access: local + LAN
…
```

The first URL uses the machine's local (mDNS) name, which keeps working when the router hands out a new IP; the snippets use it too. The name comes from System Settings → General → Sharing → Local hostname on macOS, the computer name on Windows, and the hostname on Linux when Avahi is running; elsewhere only IPs are shown. The LAN IPs are listed as fallbacks, for networks that block mDNS (guest Wi-Fi, networks with client isolation, many office networks). If two machines claim the same name, macOS renames one (`Studio-2.local`) and client configs need updating. To print a different host first, e.g. a name your router publishes or a fixed IP, set `"advertise": "studio.lan"` in `rhyolite.json` (a bare host or IP, no scheme or port).

Two flavours are printed: one using [`mcp-remote`](https://www.npmjs.com/package/mcp-remote) for clients that insist on HTTPS, and one for clients that speak HTTP directly (e.g. `claude mcp add --transport http`).

Options: `--port N` overrides the port from `rhyolite.json` (default `6270`), `--access lan|local` overrides the `access` setting in `rhyolite.json` (default `lan`: this machine and the LAN; `local`: this machine only), `--verbosity none|basic|verbose` sets the terminal output (default `verbose`): `none` prints nothing, `basic` prints only the setup info (configuration and startup banner), `verbose` also echoes every log line. `rhyolite.log` is written in all modes. `--version` prints the version.

The `"access"` key in `rhyolite.json` takes `lan` (the default) or `local` and applies to every run that doesn't pass `--access` (the macOS app always passes it, so the key never applies to app runs). The key is optional and not written by default. Both modes print the same URLs, so client configs never change when switching. After the URLs the banner states the mode: `› Access: local + LAN` or `› Access: local only`.

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

Security note: the server speaks plain HTTP, so on an untrusted network the bearer token can be sniffed. TLS is not implemented yet. Requests arriving over loopback are accepted only with a `Host` of `localhost`, a loopback IP, the machine's local name or the `advertise` host; other names are refused, which blocks DNS-rebinding attacks. In `local` mode, other machines are refused when they connect, before any HTTP is read (each such IP is logged once as `refused`), and the `Host` check applies to every connection, not only loopback ones. A `Host` equal to the IP address a connection arrived on is also accepted, so the LAN-IP URL works on this machine. Containers and VMs on the same machine (Docker reaching the host through its bridge IP, for example) connect from a different address, so `local` mode refuses them.

## macOS app

`Rhyolite.app` is a native wrapper around the CLI for people who'd rather not use a terminal. It embeds `rhyolite` and runs it; the server itself is unchanged.

```sh
make app                      # -> bin/Rhyolite.app (macOS 14+, needs Xcode or the Command Line Tools)
open bin/Rhyolite.app
```

- **First launch** opens the window, where you choose the vault folder on the **Vault** tab.
- **Later launches** open no window and start the server on the saved folder. If the folder is gone, the window opens instead. If the server fails to start (for example, the port is taken), the window opens on the **Log** tab.
- The window has a sidebar with four tabs (the window's header shows the selected tab's name, with the vault's name and the server's status below it):
  - **Clients**: every app that has used the server since the app launched, newest first: one row each with its name (as the app reports it) and IP, its last connection and, below it, its first. An app on two machines shows twice. Failed attempts are listed per IP with their count, since a refused request doesn't say which app sent it. The list survives server restarts but not quitting the app. **Clear List** empties it; an app that is still connected comes back on its next request.
  - **Server**: a switch to turn the server on or off, with its status (the switch is disabled until the folder exists), and **Allow LAN connections** (on by default). Turning it off serves only this Mac (the CLI's `--access local`); changing it while the server runs restarts the server.
  - **Vault**: the full folder path with **Change…** (only while the server is stopped), and **Show in Finder** with **Reveal**.
  - **Log**: a live log of the CLI's output, including the `mcp.json` snippets with your token. It follows new output while scrolled to the end and stays put when you scroll up. The log is kept in memory only; **Copy Log** copies it.
- Closing the window keeps the app and server running. Click the Dock icon to reopen it. **Quit** (menu or ⌘Q) stops the server first: it sends the CLI the same signal as Ctrl+C and only forces it after a timeout.

The build is ad-hoc signed, which is enough on the Mac that built it. An app downloaded from elsewhere is quarantined by Gatekeeper: right-click → Open, or run `xattr -dr com.apple.quarantine Rhyolite.app`. Developer ID signing and notarization aren't set up yet (`SIGN_IDENTITY` in `scripts/build-app.sh` is the hook for it).

## Develop

```sh
make build                    # -> bin/rhyolite
make test                     # gofmt, vet, cross-compile check, tests with -race (+ the app's Swift tests on macOS)
make cover                    # + coverage
make fuzz                     # + 30s fuzzing of path validation
make app                      # bin/Rhyolite.app (macOS only)
make release                  # dist/ binaries for macOS, Linux, Windows (CGO-free), plus Rhyolite-macos.dmg on macOS
```

`make` wraps the scripts in `scripts/`, which take extra flags (e.g. `scripts/test.sh --real-trash` also tests against the real OS trash).

The CLI's `--version` is `VERSION` when set (e.g. `VERSION=1.2.3 make build`), otherwise the version Go stamps from git: the tag on the commit, or a pseudo-version naming it. The CLI builds with Go alone; the app (`scripts/build-app.sh`) takes its version and build number from `macos/Info.plist`, which the release workflow updates, and embeds a CLI with the same version.

Logs go to `rhyolite.log` in the vault (rolled monthly to `rhyolite.YYYYMM.log`) and every line is also printed to the terminal; the token and file contents are never logged.

Each log line is [logfmt](https://brandur.org/logfmt): one event per line, space-separated `key=value` pairs in a fixed order, always starting with `ts=<RFC3339> event=<name>`. A value that is empty or contains a space, `=`, `"` or a control character is double-quoted (Go string syntax, so `\"` and `\n` escapes); everything else is bare.

| `event` | Keys after `ts` and `event` |
| --- | --- |
| `start` | `version`, `vault`, `listen`, `access` |
| `stop` | none |
| `http` | `method`, `path`, `status`, `dur_ms`, `ip`, `session` (the MCP session id; only when the request belongs to a session) |
| `client_new` | `ip`, `auth` (`ok` or `fail`: whether that first request had a valid token) |
| `client_init` | `ip`, `session`, `name`, `version` (the client app's own name and version, as it reports them when it opens an MCP session) |
| `auth_fail` | `method`, `path`, `ip` |
| `refused` | `ip` |
| `tool` | `name`, `path` (when the tool takes one), `destination` (moves), `result` (`ok` or `error`), `dur_ms`, and `error` (only on failure; truncated to 200 characters) |

`client_new` is logged once, just before the first `http`/`auth_fail` line from a client IP not seen since the server started (the set is kept in memory, so a restart announces everyone again, and it stops growing at 10,000 IPs). In a terminal that line is printed in bold; the log file never contains escape codes.

```
ts=2026-09-30T12:00:00+02:00 event=client_new ip=192.168.1.20 auth=ok
ts=2026-09-30T12:00:00+02:00 event=client_init ip=192.168.1.20 session=XK3V7Q2M name=claude-ai version=0.1.0
ts=2026-09-30T12:00:00+02:00 event=http method=POST path=/mcp status=200 dur_ms=12 ip=192.168.1.20 session=XK3V7Q2M
ts=2026-09-30T12:00:00+02:00 event=tool name=file_read path="my notes/a.md" result=ok dur_ms=1
```
