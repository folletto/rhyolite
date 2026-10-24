import SwiftUI

/// The window's content: a fixed sidebar of tabs that can't be collapsed, with the selected tab's page beside it.
/// The window's header shows the tab's name, with the vault's name below it.
struct MainView: View {
    let model: AppModel

    var body: some View {
        NavigationSplitView(columnVisibility: .constant(.all)) {
            List(Tab.allCases, selection: selection) { tab in
                Label(tab.title, systemImage: tab.symbol)
            }
            .navigationSplitViewColumnWidth(min: 180, ideal: 180, max: 180)
            .toolbar(removing: .sidebarToggle)
        } detail: {
            Group {
                switch model.selectedTab {
                case .server: ServerTab(model: model)
                case .vault: VaultTab(model: model)
                case .log: LogTab(model: model)
                }
            }
            .navigationTitle(model.selectedTab.title)
            .navigationSubtitle(model.vaultName.isEmpty ? "No vault" : model.vaultName)
        }
    }

    /// The list's selection is optional. A cleared selection keeps the current tab.
    private var selection: Binding<Tab?> {
        Binding(
            get: { model.selectedTab },
            set: { tab in
                if let tab { model.selectedTab = tab }
            }
        )
    }
}
