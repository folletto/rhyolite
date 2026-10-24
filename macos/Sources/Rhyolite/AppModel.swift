import AppKit
import Observation
import RhyoliteCore

/// What the window shows and does: the saved vault folder, the LAN access choice,
/// the selected tab and the CLI process.
@MainActor
@Observable
final class AppModel {
    private(set) var vaultPath: String
    private(set) var isConfigured: Bool
    /// The tab the window shows. Saved, so the next launch opens on it.
    var selectedTab: Tab {
        didSet { settings.selectedTab = selectedTab.rawValue }
    }
    /// Whether the server serves the LAN (`--access lan`) or only this Mac. Applied on every start.
    private(set) var allowLAN: Bool
    /// True while a running server is stopped and started again to apply a changed `allowLAN`.
    private(set) var isRestarting = false
    let server: ServerProcess

    @ObservationIgnored private let settings: Settings
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let cliURL: URL
    /// Set when the app starts to quit, so a restart still stopping the CLI does not start it again.
    @ObservationIgnored private var quitting = false

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        settings = Settings(defaults: defaults)
        vaultPath = settings.vaultPath
        isConfigured = settings.isConfigured
        selectedTab = Tab(rawValue: settings.selectedTab) ?? .server
        allowLAN = settings.allowLAN
        cliURL = Self.locateCLI()
        server = ServerProcess(executableURL: cliURL, defaults: defaults)
    }

    /// The CLI embedded in the app bundle. `RHYOLITE_CLI` overrides it, for
    /// running the bare executable during development (`swift run`).
    private static func locateCLI() -> URL {
        if let path = ProcessInfo.processInfo.environment["RHYOLITE_CLI"], !path.isEmpty {
            return URL(fileURLWithPath: path)
        }
        return Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/rhyolite")
    }

    /// Re-reads the saved path and re-checks that the folder still exists.
    func refresh() {
        vaultPath = settings.vaultPath
        isConfigured = settings.isConfigured
    }

    /// The vault folder's name, shown as the window's subtitle. Empty when no folder is chosen.
    var vaultName: String {
        vaultPath.isEmpty ? "" : URL(fileURLWithPath: vaultPath).lastPathComponent
    }

    var canChangeFolder: Bool { !server.isActive }

    /// True while the server is starting or running, which is when the Server switch is on.
    var isServerOn: Bool {
        switch server.state {
        case .starting, .running: true
        case .stopped, .stopping, .failed: false
        }
    }

    func setServer(on: Bool) {
        if on { start() } else { stop() }
    }

    /// Saves the LAN choice. A server that is starting or running is restarted, so the new access applies now.
    func setAllowLAN(_ on: Bool) {
        guard on != allowLAN else { return }
        allowLAN = on
        settings.allowLAN = on
        guard isServerOn else { return }
        isRestarting = true
        Task {
            await server.stop()
            isRestarting = false
            if !quitting { start() }
        }
    }

    /// Called when the app starts to quit, before the CLI is stopped.
    func prepareToQuit() {
        quitting = true
    }

    func chooseFolder() {
        guard canChangeFolder else { return }
        let panel = NSOpenPanel()
        panel.message = "Choose the folder Rhyolite serves"
        panel.prompt = "Choose"
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.canCreateDirectories = true
        panel.allowsMultipleSelection = false
        if isConfigured { panel.directoryURL = URL(fileURLWithPath: vaultPath) }
        guard panel.runModal() == .OK, let url = panel.url else { return }
        settings.vaultPath = url.path
        refresh()
    }

    func start() {
        refresh() // the folder may have gone away since the window last updated
        guard isConfigured else { return }
        server.start(vaultPath: vaultPath, access: allowLAN ? .lan : .local)
    }

    func stop() {
        Task { await server.stop() }
    }

    func revealVault() {
        guard isConfigured else { return }
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: vaultPath)])
    }

    func copyLog() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(server.log.text, forType: .string)
    }

    /// Stops a CLI left behind by a previous crash of the app.
    func reapOrphan() async {
        await OrphanReaper.reap(defaults: defaults, executableURL: cliURL)
    }
}
