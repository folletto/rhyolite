# AGENTS.md

Rhyolite: single-binary Go MCP server (HTTP, bearer token) exposing a folder of Markdown files (a "vault") to MCP clients. One dependency (`purego`, for macOS trash); CGO-free.

## Commands

```sh
make build     # -> bin/rhyolite
make test      # gofmt + vet + cross-compile + tests with -race, + `swift test` on macOS (run before committing)
make cover     # + coverage
make fuzz      # + 30s fuzzing of path validation
make app       # -> bin/Rhyolite.app (macOS only; needs the Swift toolchain)
make release   # dist/ for macOS/Linux/Windows (+ Rhyolite-macos.zip on macOS)
```

`make` wraps `scripts/build.sh` and `scripts/test.sh`, which take extra flags (e.g. `--real-trash` hits the real OS trash). Run locally: `bin/rhyolite <folder> [--port N] [--access lan|local] [--verbosity none|basic|verbose]`.

## Layout

- `cmd/rhyolite/`: CLI entry, startup banner.
- `internal/config/`: `rhyolite.json` (token, port, access) handling.
- `internal/server/`: MCP HTTP server; `tools.go` defines the 14 tools, `middleware.go` has bearer auth, origin guard, request logging; `listener.go` is the local-only listener for `access: local`.
- `internal/vault/`: all file operations, confined to the vault root via `os.Root`; paths, frontmatter, search, move, staleness tracker, pagination cursors.
- `internal/trash/`: OS trash per platform (macOS, freedesktop, Windows).
- `internal/logfile/`: monthly-rolling `rhyolite.log`.
- `macos/`: the native Swift app (SwiftPM, no Xcode project). `RhyoliteCore` holds settings, the log buffer and the child-process wrapper (unit-tested); `Rhyolite` is the AppKit/SwiftUI shell. The CLI is embedded in the bundle at `Contents/Helpers/rhyolite`.
- `specs/`: plans named `plan-pr-<N>.md` (N = the PR number), one per PR whose work started with a plan; simple or incremental changes don't need one (see "Plan → PR workflow"). `specs/plan-pr-1.md` is the original design: read §5 (vault rules), §6 (tools), §7 (conflicts), §12 (deviations) before changing behaviour.

## Plan → PR workflow

This workflow applies only when the work starts with planning; simple or incremental changes skip it and open the PR directly from the implementation commits. The PR number isn't known until the PR exists, so once a plan is ready:

1. Write the plan under a temporary name (e.g. `specs/plan-<topic>.md`), commit it alone on a new branch, push, and open the PR.
2. Rename the file to `specs/plan-pr-<N>.md` using the new PR number, and remove any placeholder notes about renaming.
3. Rewrite the branch so its first and only commit is the plan with its final name (`git commit --amend`), then `git push --force-with-lease`. The PR then starts with a single plan commit.

Implementation commits go on top of that first commit.

## Invariants (don't break)

- Only `.md` files and folders are visible. Hidden names (`.x`), symlinks, `rhyolite.json`/`rhyolite.log` are invisible.
- Paths are vault-relative, `/`-separated; no absolute paths or `..`. Every path goes through `vault` validation; never use raw `os` calls on user paths.
- Deletes go to the system trash, never permanent. Folder delete only if empty.
- Never log the token or file contents.
- Edits match against current file content; stale copies yield a `notice`; `expected_hash` makes edits strict.

## Conventions

- Before starting work on a new branch, fetch `origin` and make sure the branch starts from the latest `main`; update it first if it doesn't.
- Code must stay `gofmt`/`go vet` clean and pass cross-compilation for all three OSes (platform code uses build-tagged files, e.g. `system_darwin.go`).
- Add tests alongside changes (`*_test.go`, integration tests in `internal/server`). Keep the fuzz test for path validation passing.
- Go files must not reference plan files (`specs/`); keep comments self-contained.
- Update `README.md` (user-facing) when behaviour changes, and the PR's plan in `specs/` if it has one.
- The macOS app only wraps the CLI: it starts and stops it and shows its output. Never move server logic into Swift, and keep the CLI's stdout/stderr and SIGINT/SIGTERM behaviour stable, because the app relies on them (`● Listening` marks a successful start). The app runs the CLI at the default `--verbosity verbose`, so its output view shows the setup lines plus the logfmt `event=` log lines; never make it pass `--verbosity none`, which suppresses `● Listening` so a start is never detected (`basic` is safe).
- Swift builds only on macOS. Run swift commands outside restrictive sandboxes (they write to system caches).
- Not implemented yet: TLS (plain HTTP only).
