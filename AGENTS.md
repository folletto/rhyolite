# AGENTS.md

Rhyolite: single-binary Go MCP server (HTTP, bearer token) exposing a folder of Markdown files (a "vault") to MCP clients. One dependency (`purego`, for macOS trash); CGO-free.

## Commands

```sh
make build     # -> bin/rhyolite
make test      # gofmt + vet + cross-compile + tests with -race (run before committing)
make cover     # + coverage
make fuzz      # + 30s fuzzing of path validation
make release   # dist/ for macOS/Linux/Windows
```

`make` wraps `scripts/build.sh` and `scripts/test.sh`, which take extra flags (e.g. `--real-trash` hits the real OS trash). Run locally: `bin/rhyolite <folder> [--port N]`.

## Layout

- `cmd/rhyolite/`: CLI entry, startup banner.
- `internal/config/`: `rhyolite.json` (token, port) handling.
- `internal/server/`: MCP HTTP server; `tools.go` defines the 14 tools, `middleware.go` has bearer auth, origin guard, request logging.
- `internal/vault/`: all file operations, confined to the vault root via `os.Root`; paths, frontmatter, search, move, staleness tracker, pagination cursors.
- `internal/trash/`: OS trash per platform (macOS, freedesktop, Windows).
- `internal/logfile/`: monthly-rolling `rhyolite.log`.
- `specs/`: plans, one per PR, named `plan-pr-<N>.md` (N = the PR number). Each PR has its own plan; create it if missing. `specs/plan-pr-1.md` is the original design: read §5 (vault rules), §6 (tools), §7 (conflicts), §12 (deviations) before changing behaviour.

## Invariants (don't break)

- Only `.md` files and folders are visible. Hidden names (`.x`), symlinks, `rhyolite.json`/`rhyolite.log` are invisible.
- Paths are vault-relative, `/`-separated; no absolute paths or `..`. Every path goes through `vault` validation; never use raw `os` calls on user paths.
- Deletes go to the system trash, never permanent. Folder delete only if empty.
- Never log the token or file contents.
- Edits match against current file content; stale copies yield a `notice`; `expected_hash` makes edits strict.

## Conventions

- Code must stay `gofmt`/`go vet` clean and pass cross-compilation for all three OSes (platform code uses build-tagged files, e.g. `system_darwin.go`).
- Add tests alongside changes (`*_test.go`, integration tests in `internal/server`). Keep the fuzz test for path validation passing.
- Go files must not reference plan files (`specs/`); keep comments self-contained.
- Update `README.md` (user-facing) and the PR's plan in `specs/` when behaviour changes.
- Not implemented yet: TLS (plain HTTP only).
