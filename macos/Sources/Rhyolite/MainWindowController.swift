import AppKit
import SwiftUI

/// The one window. Closing it only hides it: the controller stays alive and is reused.
@MainActor
final class MainWindowController: NSWindowController {
    private static let frameName = "RhyoliteMainWindow"

    init(model: AppModel) {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 720, height: 480),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "Rhyolite"
        window.isReleasedWhenClosed = false
        window.contentMinSize = NSSize(width: 600, height: 380)
        // An explicit toolbar gives the window the tall unified header, with the sidebar running full height
        // beside it. SwiftUI fills in the header's title and subtitle from the tabs.
        window.toolbar = NSToolbar(identifier: "RhyoliteMainToolbar")
        window.toolbarStyle = .unified
        let hostingView = NSHostingView(rootView: MainView(model: model))
        hostingView.sceneBridgingOptions = [.title, .toolbars]
        window.contentView = hostingView
        window.setContentSize(NSSize(width: 720, height: 480))
        let hadSavedFrame = UserDefaults.standard.object(forKey: "NSWindow Frame \(Self.frameName)") != nil
        window.setFrameAutosaveName(Self.frameName)
        if !hadSavedFrame { window.center() }
        super.init(window: window)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("not used") }

    var isVisible: Bool { window?.isVisible == true }

    /// Shows the window (restoring it if minimized) and brings it to the front.
    func present() {
        guard let window else { return }
        if window.isMiniaturized { window.deminiaturize(nil) }
        showWindow(nil)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate()
    }
}
