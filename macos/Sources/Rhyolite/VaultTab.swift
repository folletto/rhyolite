import SwiftUI

/// The Vault tab: the chosen folder with a button to change it, and a button to show it in Finder.
struct VaultTab: View {
    let model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Form {
                Section {
                    LabeledContent {
                        Button("Change…") { model.chooseFolder() }
                            .disabled(!model.canChangeFolder)
                    } label: {
                        Text("Folder")
                        Text(model.vaultPath.isEmpty ? "No folder chosen" : model.vaultPath)
                            .lineLimit(1)
                            .truncationMode(.middle)
                            .textSelection(.enabled)
                    }
                    LabeledContent("Show in Finder") {
                        Button("Reveal") { model.revealVault() }
                            .disabled(!model.isConfigured)
                    }
                } footer: {
                    if let hint = folderHint {
                        Text(hint.text)
                            .foregroundStyle(hint.isProblem ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
                    }
                }
            }
            .formStyle(.grouped)
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var folderHint: (text: String, isProblem: Bool)? {
        if model.vaultPath.isEmpty { return ("Choose the folder to serve before starting.", false) }
        if !model.isConfigured { return ("This folder no longer exists. Choose another one.", true) }
        if model.server.isActive { return ("Stop the server to change the folder.", false) }
        return nil
    }
}
