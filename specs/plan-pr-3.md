# Rhyolite.app: implementation plan

A native macOS app, written in Swift, that wraps the `rhyolite` CLI. The app picks the vault folder, starts and stops the CLI as a child process, and shows the CLI's output. It does not reimplement any server logic: the Go binary is embedded in the app bundle and run unchanged.

## 1. Goals and non-goals

**Goals**
- A folder in this repo, `macos/`, holding the app's Swift sources.
- `make app` builds `bin/Rhyolite.app` from the command line, through the existing root `Makefile` and `scripts/`. There is no second Makefile and no Xcode project to open.
- The app embeds `rhyolite` and runs it, as described in the specs in §4.

**Non-goals (for this PR)**
- No changes to the Go code. The app relies only on the CLI's current interface: `rhyolite <folder>`, stdout/stderr, and SIGINT/SIGTERM for a clean stop.
- No App Store build, no App Sandbox, no notarization. The build is ad-hoc signed (§3.4).
- No editing of `rhyolite.json` (port, host, token) from the app. Users edit the file as before.
- No menu bar item and no launch at login. Both can come later.

## 2. Layout

```
macos/
  Package.swift                 SwiftPM package: one executable target, one test target
  Info.plist                    bundle metadata; version strings are filled in at build time
  Rhyolite.entitlements         empty for now (no sandbox); present so signing is explicit
  Resources/AppIcon.icns        app icon (placeholder until a real one exists)
  Sources/Rhyolite/
    main.swift                  NSApplication bootstrap (AppKit lifecycle, see §5.1)
    AppDelegate.swift           launch, dock reopen, quit handling
    MainWindowController.swift  the single window, hosting the SwiftUI view
    MainView.swift              SwiftUI: folder picker, Start/Stop, status, log
    Settings.swift              vault path persisted in UserDefaults, validation
    ServerProcess.swift         owns the child process: start, stop, output, state
    LogBuffer.swift             bounded line buffer fed by the process pipes
  Tests/RhyoliteTests/
    SettingsTests.swift
    LogBufferTests.swift
    ServerProcessTests.swift    runs a fake CLI (a shell script) to test start/stop/output
```

**Why SwiftPM rather than an `.xcodeproj`:** a package is plain text, diffs well, and builds with `swift build`. That covers the command-line requirement without `xcodebuild` project files. SwiftPM doesn't produce `.app` bundles, so the build script assembles the bundle itself (§3). That only takes a few lines.

Repo-level edits:
- `Makefile`: new `app` target. `clean` also removes `macos/.build`.
- `scripts/build.sh`: new `--app` flag.
- `scripts/test.sh`: runs `swift test` on macOS when a Swift toolchain is present (§7).
- `.gitignore`: add `/macos/.build/`.
- `README.md`, `AGENTS.md`: document the app and the new command.

## 3. Build

### 3.1 Make target

```make
app:
	@scripts/build.sh --app
```

`make release` stays as it is for the CLI binaries. When run on macOS, it also produces `dist/Rhyolite-macos.zip` by calling the same `--app` path into `dist/`. On other OSes it skips the app with a one-line notice. The Go cross-compile in `test.sh` is unchanged.

### 3.2 `scripts/build.sh --app` steps

1. Require macOS (`uname -s` = `Darwin`) and `swift`. Otherwise exit with a clear message.
2. Build a **universal** CLI: `go build` for `darwin/arm64` and `darwin/amd64` with the existing `LDFLAGS`/`VERSION`, then `lipo -create` into a temp dir.
3. Build the app executable: `swift build -c release --package-path macos --arch arm64 --arch x86_64`. The binary path comes from `--show-bin-path`.
4. Assemble `bin/Rhyolite.app`:
   ```
   Rhyolite.app/Contents/
     Info.plist                (copied, with CFBundleShortVersionString / CFBundleVersion set from $VERSION via plutil)
     MacOS/Rhyolite            Swift executable
     Helpers/rhyolite          embedded CLI
     Resources/AppIcon.icns
   ```
   The CLI goes in `Contents/Helpers/`, **not** `Contents/MacOS/`. The default macOS filesystem is case-insensitive, so `MacOS/rhyolite` and `MacOS/Rhyolite` would be the same file.
5. Sign inside-out with an ad-hoc identity (`codesign --force --options runtime --timestamp=none -s -`): first `Helpers/rhyolite`, then the bundle. `SIGN_IDENTITY` can override `-`, so a Developer ID can be used later without changing the script.
6. Print `built bin/Rhyolite.app (<version>)`, matching the existing output style.

The script stays POSIX `sh`, like the rest of `scripts/`.

### 3.3 Deployment target

macOS 14 (Sonoma). The minimum is set in `Package.swift` and in `LSMinimumSystemVersion`. macOS 14 allows SwiftUI with the `@Observable` macro and current `NSOpenPanel` behaviour. Swift 6 language mode, with strict concurrency.

### 3.4 Signing and Gatekeeper

Ad-hoc signing is enough to run the app on the machine that built it. A copy downloaded on another Mac gets quarantined, so the README explains right-click → Open (or `xattr -dr com.apple.quarantine`). Developer ID signing and notarization are left for a later PR. `SIGN_IDENTITY` is the hook for it.

## 4. Behaviour (the specs)

| # | Spec | Implementation |
|---|---|---|
| 1 | Wraps the embedded CLI, no duplicated logic | `ServerProcess` runs `Bundle.main` → `Contents/Helpers/rhyolite` with the vault path as its only argument. All config handling (`rhyolite.json`, token, port) stays in the CLI. |
| 2 | Launch without a valid configuration opens the window | In `applicationDidFinishLaunching`, `AppDelegate` checks `Settings.isConfigured`. If it is false, the window opens and nothing is started. This covers the first launch, a missing path (key absent or empty), and a broken path (the folder no longer exists or isn't a directory). |
| 3 | Launch with a valid configuration doesn't open the window and auto-starts `rhyolite` | If `isConfigured` is true, no window is shown and `ServerProcess.start()` is called right away. If the auto-start ends in `failed` (for example, port in use or bad `rhyolite.json`), the window opens so the error in the log is visible. Otherwise the failure would go unnoticed. |
| 3a | Closing the window doesn't quit the app; only an explicit quit does | `applicationShouldTerminateAfterLastWindowClosed` → `false`. Closing the window only hides it (`isReleasedWhenClosed = false`, controller kept), and the server keeps running. The app quits only via the Rhyolite menu ▸ Quit, ⌘Q, or the dock menu's Quit. All of them go through `applicationShouldTerminate` (spec 7). |
| 3b | Clicking the dock icon opens the window if it isn't open | `applicationShouldHandleReopen(_:hasVisibleWindows:)` shows the single window and brings it to the front (`makeKeyAndOrderFront` + `NSApp.activate`). If the window is already open, it is only brought forward. The same controller is reused, so the window is never duplicated. |
| 4 | A Start/Stop button | One button whose label and action follow `ServerProcess.state` (§5.3). |
| 5 | Start is disabled when not configured | Disabled unless the path is non-empty **and** exists and is a directory. This is checked live: on edit, on window focus, and just before starting, in case the folder was removed. |
| 6 | After a successful start, a log shows the CLI's output and grows as more is written | stdout and stderr are both read and appended to `LogBuffer`. A monospaced, selectable, auto-scrolling text view sits under the controls. |
| 7 | On quit, stop the CLI safely first | `applicationShouldTerminate` returns `.terminateLater` while the process runs, stops it (§5.4), then calls `reply(toApplicationShouldTerminate: true)`. |

Launch decision, in order:

```
applicationDidFinishLaunching
  ├─ clean up an orphaned CLI from a previous crash (§5.4)
  ├─ isConfigured == false → show window, don't start
  └─ isConfigured == true  → no window, start()
                              └─ ends in failed → show window
```

## 5. Design

### 5.1 App lifecycle: AppKit, with SwiftUI inside the window

SwiftUI's `App`/`WindowGroup` opens a window at launch and gives little control over "open only on first run" and "reopen on dock click". So the app uses a plain `NSApplication` with `AppDelegate`, and one `NSWindow` whose content is an `NSHostingView(rootView: MainView(...))`. That keeps launch, reopen and terminate in standard AppKit delegate methods, and the UI itself is still SwiftUI.

It is a regular dock app (`LSUIElement` absent). A minimal main menu is built in code: About, Quit ⌘Q, and Window ▸ Show Rhyolite ⌘0.

### 5.2 Settings

- `UserDefaults` key `vaultPath` (String). Nothing else is persisted.
- `isConfigured` = path is non-empty, `FileManager.fileExists(atPath:isDirectory:)` returns true, and it is a directory.
- The UI has a read-only path field, a **Choose…** button (an `NSOpenPanel` limited to directories, with folder creation allowed), and an inline message when the saved path no longer exists.
- The folder can't be changed while the server is running: the controls are disabled, with a hint saying "Stop to change the folder".
- The app isn't sandboxed, so a plain path is enough and no security-scoped bookmarks are needed.

### 5.3 `ServerProcess` state machine

```
stopped ──start──▶ starting ──"▴ Listening on" seen──▶ running
   ▲                  │                                   │
   │                  └──exit──▶ failed(code) ◀──exit─────┤ (unexpected)
   └──────────────── stopping ◀──────────stop─────────────┘
```

- An `@Observable`, `@MainActor` class holding `state` and `log`.
- **Start:** builds a `Process` with `executableURL` = embedded CLI, `arguments = [vaultPath]`, `currentDirectoryURL` = vault, and one `Pipe` for stdout and one for stderr. If `run()` throws (for example, a missing helper), the state becomes `failed` and the error goes into the log.
- **Success detection:** the CLI prints `▴ Listening on …` once it has bound the port. Seeing that line moves the state to `running`. If the process exits first (port in use, bad `rhyolite.json`, and so on), the state becomes `failed(exitCode)`. The log stays visible so the CLI's error message can be read, and Start is enabled again.
- **Button:** `stopped`/`failed` → "Start" (enabled iff configured). `starting`/`running` → "Stop". `stopping` → "Stopping…" (disabled).
- A status line shows a dot and a word (Stopped / Starting / Running / Failed (exit N)).

### 5.4 Stopping safely

The CLI already handles SIGINT/SIGTERM. It stops accepting connections, calls `http.Server.Shutdown` with a 5 s timeout, and writes `stop` to `rhyolite.log`.

1. `process.interrupt()` (SIGINT), the same as Ctrl+C in a terminal.
2. Wait for `terminationHandler`, up to **8 s** (the CLI's 5 s shutdown plus margin).
3. If the process is still alive, send `process.terminate()` (SIGTERM) and wait 2 s more.
4. As a last resort, send `kill(pid, SIGKILL)`, and write a line to the log saying so.

`stop()` is `async` and returns once the process has exited, so quit (§4 #7) can await it before replying to `applicationShouldTerminate`.

**Orphans:** if the app crashes or is force-quit, the child is not killed automatically. Two mitigations:
- the app records the child PID in `UserDefaults` while it runs and clears it on exit. On the next launch, if the PID is still alive **and** its executable path is our embedded CLI (checked via `proc_pidpath`), the app sends it SIGINT before doing anything else;
- a follow-up option on the Go side, outside this PR: a `--parent-pid`/stdin-EOF watchdog. It would make cleanup exact, but it changes the CLI, which this PR avoids.

### 5.5 Log handling

- `readabilityHandler` on each pipe reads data off the main thread. Bytes are decoded as UTF-8; a partial trailing line is kept until its newline arrives, so multi-byte characters like `▴` are never split. Complete lines are then hopped to the main actor and appended.
- The CLI writes to `os.Stdout` unbuffered, so lines appear as soon as they are printed, and no pseudo-terminal is needed. The CLI prints no ANSI colour codes.
- `LogBuffer` keeps the last **5,000 lines**, dropping the oldest, so a long-running session can't grow without bound.
- The log is cleared on each Start, and kept after Stop/failure for reading.
- The log is **in memory only**: the CLI banner prints the bearer token in the ready-to-paste `mcp.json` snippets. Showing it in the user's own window is the point, but the app never writes the log to disk. The CLI's own `rhyolite.log` stays the persistent log, and it already excludes the token.
- Small extras with little cost: a **Copy** button for the whole log, and a **Reveal in Finder** button for the vault folder.

### 5.6 Window layout (single window, ~640×480, resizable)

```
┌ Rhyolite ───────────────────────────────────────────┐
│ Vault folder  [ /Users/me/Notes            ] Choose…│
│ ● Running                                   [ Stop ]│
├─────────────────────────────────────────────────────┤
│ ▴ Reading rhyolite.json                             │
│ ▴ Vault: /Users/me/Notes                            │
│ ▴ Listening on http://192.168.1.20:6270/mcp         │
│ …                                       (log, mono) │
└─────────────────────────────────────────────────────┘
```

The window size and position are remembered with `setFrameAutosaveName`.

## 6. Edge cases

- **Folder deleted or renamed while running:** the CLI keeps serving its open root, so nothing is done mid-run. The next Start re-validates the path and disables the button if it's gone.
- **Port already in use:** the CLI exits non-zero with its error. The state becomes `failed`, and the error is visible in the log.
- **Embedded helper missing or not executable** (broken bundle): the state becomes `failed` with a clear log line. Start is still allowed, so the error stays visible.
- **Quit during `starting`:** same path as stop. SIGINT is safe before the listener is up, because the context is cancelled and `run` returns.
- **Logout/shutdown:** AppKit sends `applicationShouldTerminate`, so the same safe stop runs. The 8 s budget stays inside macOS's termination grace period.
- **Two app instances:** prevented by the bundle (LaunchServices activates the running instance instead of opening a second one).

## 7. Tests

- `swift test --package-path macos`, wired into `scripts/test.sh` and therefore `make test`. It runs only when `uname` is Darwin and `swift` is on `PATH`, so Linux CI and Go-only contributors are unaffected. Otherwise it prints `skipping macOS app tests`.
- `SettingsTests`: the `isConfigured` cases (empty, missing, file instead of folder, valid dir), using a temporary `UserDefaults` suite.
- `LogBufferTests`: line splitting across chunk boundaries, UTF-8 split in the middle of `▴`, and the 5,000-line cap.
- `ServerProcessTests` injects the executable URL and points it at shell-script fakes:
  - one prints `▴ Listening on …` and waits for SIGINT → expect `starting → running → stopping → stopped`;
  - one exits 1 immediately → expect `failed(1)`, with its stderr in the log;
  - one ignores SIGINT → expect escalation to SIGTERM/SIGKILL within the timeouts (shortened through an injected timeout for test speed).
- The launch decision (§4) is factored into a pure function `launchAction(isConfigured:) -> .showWindow | .autoStart` and unit-tested for each settings case.
- Manual checklist in the PR description: first launch shows the window and doesn't start; a relaunch with a valid folder shows no window and the server is running; a relaunch after deleting the folder shows the window and doesn't start; closing the window keeps the app and server running; a dock click reopens the window; Start is disabled with no or an invalid folder; the log streams; ⌘Q while running leaves `stop` in `rhyolite.log` and no `rhyolite` left in `ps`.

## 8. Docs

- `README.md`: a short "macOS app" section covering `make app` → `bin/Rhyolite.app`, what the window does, and the Gatekeeper note.
- `AGENTS.md`: add `make app` under Commands, `macos/` under Layout, and a convention: "the app wraps the CLI; never move server logic into Swift".

## 9. Work order

1. Scaffold `macos/` (package, Info.plist, empty window), plus `build.sh --app` producing a launchable, signed bundle. Verify with `make app && open bin/Rhyolite.app`.
2. `Settings` + folder picker + first-run/reopen behaviour.
3. `ServerProcess` + `LogBuffer` + Start/Stop + log view.
4. Safe quit + orphan cleanup.
5. Tests, `test.sh` wiring, `.gitignore`, `clean`, `release` zip.
6. README / AGENTS.
