import AppKit
import RhyoliteCore

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let model = AppModel()
    private lazy var windowController = MainWindowController(model: model)
    private var terminating = false

    // MARK: Launch

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.mainMenu = buildMainMenu()

        // A server that fails while the window is hidden (including a failed
        // auto-start) brings the window up on the Log tab, so the error is seen.
        model.server.onStateChange = { [weak self] state in
            guard let self, case .failed = state, !windowController.isVisible else { return }
            model.selectedTab = .log
            windowController.present()
        }
        NotificationCenter.default.addObserver(
            forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.model.refresh() }
        }

        Task {
            await model.reapOrphan()
            model.refresh()
            switch launchAction(isConfigured: model.isConfigured) {
            case .showWindow: windowController.present()
            case .autoStart: model.start()
            }
        }
    }

    // MARK: Window

    /// Closing the window never quits the app; only an explicit Quit does.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    /// Dock icon clicked.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        windowController.present()
        return false
    }

    @objc private func showMainWindow(_ sender: Any?) {
        windowController.present()
    }

    // MARK: Quit

    /// Stops the CLI first, then lets the app quit.
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        model.prepareToQuit()
        guard model.server.isActive else { return .terminateNow }
        if !terminating {
            terminating = true
            Task {
                await model.server.stop()
                NSApp.reply(toApplicationShouldTerminate: true)
            }
        }
        return .terminateLater
    }

    // MARK: Menu

    private func buildMainMenu() -> NSMenu {
        let main = NSMenu()

        let app = submenu(of: main, title: "Rhyolite")
        app.addItem(withTitle: "About Rhyolite", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        app.addItem(.separator())
        app.addItem(withTitle: "Hide Rhyolite", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let hideOthers = app.addItem(withTitle: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        app.addItem(withTitle: "Show All", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: "")
        app.addItem(.separator())
        app.addItem(withTitle: "Quit Rhyolite", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")

        let edit = submenu(of: main, title: "Edit")
        edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")

        let window = submenu(of: main, title: "Window")
        window.addItem(withTitle: "Rhyolite", action: #selector(showMainWindow(_:)), keyEquivalent: "0").target = self
        window.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        window.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        NSApp.windowsMenu = window

        return main
    }

    private func submenu(of main: NSMenu, title: String) -> NSMenu {
        let item = main.addItem(withTitle: title, action: nil, keyEquivalent: "")
        let menu = NSMenu(title: title)
        main.setSubmenu(menu, for: item)
        return menu
    }
}
