# macOS app: unified header, Clients tab, Vault list, new icon

The window's header was the legacy title strip: the window drew its content under it but had no toolbar, so the Server tab's in-page title was clipped and the sidebar slid under the strip. The Vault tab was a freeform stack unlike the Server tab, and the app could not show which apps use the server. This PR fixes the header, gives the Vault tab the Server tab's grouped list, adds a Clients tab, and replaces the app icon. The CLI gains two log fields so the app can name clients; nothing else in the server changes.

## 1. Goals and non-goals

**Goals**
- The macOS 26 window layout: a tall unified header, a full-height sidebar, the tab's name as the title and the vault's name and server status as the subtitle.
- A Vault tab built from the same grouped list (`Form`, `.formStyle(.grouped)`) as the Server tab.
- A Clients tab listing every app that used the server since the app launched.

**Non-goals**
- No server logic moves into Swift: the client list is built only from the CLI's log lines.
- Clients are not saved to disk; the list lasts while the app runs.
- No per-client actions (blocking, revoking tokens).

## 2. Window header

- The window gets an explicit `NSToolbar` with `toolbarStyle = .unified`, and keeps `.fullSizeContentView`. Without a toolbar the window keeps the legacy title strip.
- The content stays an `NSHostingView`, with `sceneBridgingOptions = [.title, .toolbars]`, so SwiftUI's `.navigationTitle` and `.navigationSubtitle` fill the header. (An `NSHostingController` bridged only the subtitle.)
- Title: the selected tab's name. Subtitle: "<vault name> · <status>", e.g. "Notes · Running", or "No vault · Stopped". The status text is shared with the Server tab's status row.
- The Server tab's in-page large title is removed.

## 3. Vault tab

- One section: a "Folder" row whose label shows the full path below it (selectable, middle-truncated), with "Change…" (disabled while the server runs); and a "Show in Finder" row with "Reveal".
- The existing folder hints move to the section footer, red when the folder no longer exists.

## 4. Clients tab

- First in the sidebar: Clients, Server, Vault, Log. Symbol `laptopcomputer.and.iphone`.
- A client is an app name at an IP: the same app on two machines, or two apps on one machine, are separate rows. Failed attempts are grouped per IP, since a refused request never reaches MCP and so has no app name.
- One grouped section, one compact row per client, newest first: an icon, the name with the IP below it, and the last connection with the first below it. Failed rows show a red warning icon, "Unknown app", and the attempt count after the IP; the section footer explains the token. "Clear List" empties the list; an app with an open session comes back on its next request.
- The list lives in `AppModel`: it survives server stops and starts, not quitting the app. It is capped at 200 rows.

## 5. CLI log fields

- New event `client_init` with `ip`, `session`, `name`, `version`, logged by an MCP middleware when an app opens a session (`initialize`). The name and version are what the app reports in `clientInfo`.
- The MCP layer sees headers but not the remote address, so `requestLog` sets an internal `X-Rhyolite-Client-Ip` header to the connection's IP, always overwriting any value the client sent.
- `http` lines gain `session` (the MCP session id) when the request belongs to a session, taken from the response for `initialize` and from the request otherwise. The session id is not a credential: every request still needs the bearer token, and the token is never logged.

## 6. App core

- `ClientList` (in `RhyoliteCore`) parses logfmt lines, including Go-quoted values, and uses the CLI's `ts`. `client_init` creates or refreshes a row and records the session's owner; an `http` line in a known session (not 401) updates that row's last connection; `auth_fail` updates the IP's failed row.
- `ServerProcess.onLines` hands each batch of output lines to the app after logging them.

## 7. App icon

- `macos/Resources/AppIcon.icns` is replaced. `scripts/build.sh` copies it with `cp -X`, because codesign rejects the extended attributes (Finder info, resource fork) a downloaded or Finder-edited icon carries.

## 8. Files

- `internal/server/middleware.go`, `internal/server/server.go`, `internal/server/clients_test.go`: the log fields and their tests.
- `macos/Sources/Rhyolite/MainWindowController.swift`, `MainView.swift`: toolbar and header.
- `macos/Sources/Rhyolite/ServerTab.swift`, `VaultTab.swift`, `ClientsTab.swift`, `Tab.swift`, `AppModel.swift`: tabs and model.
- `macos/Sources/RhyoliteCore/ClientList.swift`, `ServerProcess.swift`, and `macos/Tests/RhyoliteTests/ClientListTests.swift`, `ServerProcessTests.swift`.
- `macos/Resources/AppIcon.icns`, `scripts/build.sh`.
- `README.md`: the log events table and the macOS app section.

## 9. Testing

- Automated: `make test` (Go tests including `client_init`, the session on `http` lines and the forged IP header; `swift test` including `ClientListTests`), and `make app`.
- Manual checklist, on the built app:
  - [ ] The header is tall and unified; the traffic lights sit in the sidebar and no tab is under them; the title and subtitle sit over the content; empty header space drags the window.
  - [ ] The subtitle shows the vault's name and follows the server's status.
  - [ ] Vault tab: Change… is disabled while the server runs; Reveal opens Finder; the missing-folder hint is red.
  - [ ] Clients tab: connecting an MCP app adds a row with its name and IP; a request with a wrong token adds a failed row with a count; Clear List empties the list and a connected app returns on its next request; the list survives a server restart.
  - [ ] The new icon shows in the Dock and Finder.
