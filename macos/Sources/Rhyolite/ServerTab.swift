import RhyoliteCore
import SwiftUI

/// The Server tab: the vault's name, the Server switch with the server's status, and the LAN access switch.
struct ServerTab: View {
    let model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(model.vaultName.isEmpty ? "No vault" : model.vaultName)
                .font(.largeTitle)
                .lineLimit(1)
                .truncationMode(.middle)
            Form {
                Section {
                    Toggle("Server", isOn: serverBinding)
                        .toggleStyle(.switch)
                        .disabled(isServerToggleDisabled)
                    LabeledContent("Status") {
                        HStack(spacing: 6) {
                            Circle()
                                .fill(statusColor)
                                .frame(width: 10, height: 10)
                            Text(statusText)
                        }
                    }
                } footer: {
                    if !model.isConfigured {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(unusableFolderHint)
                            Button("Open Vault Settings") { model.selectedTab = .vault }
                                .buttonStyle(.link)
                        }
                    }
                }

                Section {
                    Toggle("Allow LAN connections", isOn: lanBinding)
                        .toggleStyle(.switch)
                        .disabled(model.isRestarting || model.server.state == .stopping)
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(lanCaption)
                        if model.isServerOn {
                            Text("Changing this restarts the running server.")
                        }
                    }
                }
            }
            .formStyle(.grouped)
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var serverBinding: Binding<Bool> {
        Binding(get: { model.isServerOn }, set: { model.setServer(on: $0) })
    }

    private var lanBinding: Binding<Bool> {
        Binding(get: { model.allowLAN }, set: { model.setAllowLAN($0) })
    }

    /// The switch is locked while the server stops or restarts, and while it is off with no usable folder.
    private var isServerToggleDisabled: Bool {
        model.server.state == .stopping || model.isRestarting || (!model.isServerOn && !model.isConfigured)
    }

    private var unusableFolderHint: String {
        model.vaultPath.isEmpty
            ? "Choose a vault folder to start the server."
            : "The vault folder no longer exists."
    }

    private var lanCaption: String {
        model.allowLAN
            ? "Other devices on your network can connect."
            : "Only apps on this Mac can connect."
    }

    private var statusText: String {
        switch model.server.state {
        case .stopped: "Stopped"
        case .starting: "Starting…"
        case .running: "Running"
        case .stopping: "Stopping…"
        case .failed(let why): "Failed (\(why))"
        }
    }

    private var statusColor: Color {
        switch model.server.state {
        case .stopped: .gray
        case .starting, .stopping: .orange
        case .running: .green
        case .failed: .red
        }
    }
}
