/// The sidebar tabs of the main window, in sidebar order.
enum Tab: String, CaseIterable, Identifiable {
    case clients
    case server
    case vault
    case log

    var id: Tab { self }

    var title: String {
        switch self {
        case .clients: "Clients"
        case .server: "Server"
        case .vault: "Vault"
        case .log: "Log"
        }
    }

    /// The SF Symbol shown beside the title in the sidebar.
    var symbol: String {
        switch self {
        case .clients: "laptopcomputer.and.iphone"
        case .server: "server.rack"
        case .vault: "folder"
        case .log: "text.alignleft"
        }
    }
}
