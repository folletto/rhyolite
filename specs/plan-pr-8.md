# macOS app: sidebar with Server, Vault and Log tabs

Today the app's window is a single pane: a vault folder row, a start/stop row with the status, "Reveal in Finder" and "Copy Log" buttons, and the log view. The app always runs the CLI with only the vault path, so the server always serves the LAN. This PR reorganises the window into a sidebar and a body, and adds an access toggle. The CLI does not change: the app passes the existing `--access lan|local` flag.

## 1. Goals and non-goals

**Goals**
- A sidebar with Server, Vault and Log tabs, so each part of the window has its own place.
- An "Allow LAN connections" toggle that switches the server between serving the LAN and serving only this machine. The choice is applied on every start, and changing it while the server runs restarts the server.
- A log view that starts at the end and keeps its place: it follows new lines only while it is scrolled to the end.

**Non-goals**
- No CLI changes. The app passes the existing `--access` flag.
- The vault name and status are not shown in the window title or the sidebar footer. The title stays "Rhyolite".
- Changing the vault while the server runs is still not allowed.

## 2. Window and sidebar

- `NavigationSplitView` with a fixed-width sidebar that cannot be collapsed. The tabs use SF Symbols: Server (`server.rack`), Vault (`folder`) and Log (`text.alignleft`).
- Window size about 720×480, with a minimum of about 600×380. The full-size content view style (`fullSizeContentView`) makes the sidebar look native.
- The selected tab lives on `AppModel` and is remembered across launches (`UserDefaults`).
- When the server fails while the window is hidden, the window is presented, as today, and switches to the Log tab.

## 3. Server tab

- A large title showing the vault folder name (the last path component), or "No vault" when none is chosen.
- A "Server" toggle with a status dot and text: Stopped, Starting…, Running, Stopping… or Failed (reason). Switching it on starts the server, and switching it off stops it. It is disabled while stopping or when the vault is not usable; in that case a hint links to the Vault tab.
- An "Allow LAN connections" toggle, on by default. On passes `--access lan`; off passes `--access local`. The choice is saved in `UserDefaults` and passed on every start.
- Changing the toggle while the server runs restarts the server automatically. The toggle is disabled during the restart, and the status shows Stopping… then Starting….

## 4. Vault tab

- The full vault path, selectable and middle-truncated.
- A "Change…" button, using the existing `NSOpenPanel` picker. It is disabled while the server is running, with the hint "Stop the server to change the folder." The existing "choose a folder" and "folder no longer exists" hints are kept.
- A "Reveal in Finder" button.

## 5. Log tab

- The log view fills the tab. The "Copy Log" button moves here.
- The view starts scrolled to the end: on first display, and whenever the tab is shown again.
- When the view is at the end, new lines keep it pinned to the end. When the user has scrolled up, new lines do not move the scroll position.
- `LogTextView` appends only the new lines to the text storage, instead of replacing the whole string, which can make the view jump. It rebuilds the text only when the buffer was cleared or trimmed past its 5000-line cap, and keeps the scroll position when the view is not at the end.
- `LogBuffer` gains a monotonic count of the lines ever appended, so the view knows how many lines are new.

## 6. Files

- `macos/Sources/Rhyolite/MainView.swift`: the split view, plus `ServerTab`, `VaultTab`, `LogTab` and `LogTextView` (these may go in separate files).
- `macos/Sources/Rhyolite/AppModel.swift`: the selected tab, `allowLAN`, `setServer(on:)`, and the restart on access change.
- `macos/Sources/Rhyolite/MainWindowController.swift`, `AppDelegate.swift`: window size and style, and the switch to the Log tab on failure.
- `macos/Sources/RhyoliteCore/Settings.swift`: `allowLAN`, default true.
- `macos/Sources/RhyoliteCore/ServerProcess.swift`: `start(vaultPath:access:)` adds `--access lan|local`.
- `macos/Sources/RhyoliteCore/LogBuffer.swift`: the monotonic appended-lines count.
- Tests: `SettingsTests` (`allowLAN` default and round trip), `ServerProcessTests` (arguments include `--access`), `LogBufferTests` (count).
- `README.md`: the macOS app section.

## 7. Testing

- Automated: `make test` (includes `swift test`), and `make app` to build `bin/Rhyolite.app`.
- Manual checklist, on the built app:
  - [ ] Turn the server on and off with the "Server" toggle.
  - [ ] While the server runs, turn "Allow LAN connections" off. The server restarts, and the banner in the log shows `› Access: local only`.
  - [ ] While the server runs, "Change…" on the Vault tab is disabled.
  - [ ] "Reveal in Finder" opens the vault folder in Finder.
  - [ ] On the Log tab, with the view at the end, new lines keep it pinned to the end. After scrolling up, new lines do not move it.
  - [ ] Leaving the Log tab and coming back starts it scrolled to the end.
  - [ ] With the window hidden, a server failure brings the window forward on the Log tab.
