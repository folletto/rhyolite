import AppKit
import Observation
import RhyoliteCore

/// What the window shows and does: the saved vault folder plus the CLI process.
@MainActor
@Observable
final class AppModel {
    private(set) var vaultPath: String
    private(set) var isConfigured: Bool
    let server: ServerProcess

    @ObservationIgnored private let settings: Settings
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let cliURL: URL

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        settings = Settings(defaults: defaults)
        vaultPath = settings.vaultPath
        isConfigured = settings.isConfigured
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

    var canChangeFolder: Bool { !server.isActive }

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
        server.start(vaultPath: vaultPath)
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
