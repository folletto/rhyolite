import SwiftUI

/// The Vault tab: the chosen folder, with buttons to change it and to show it in Finder.
struct VaultTab: View {
    let model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Vault folder")
                .font(.headline)
            HStack(spacing: 8) {
                Text(model.vaultPath.isEmpty ? "No folder chosen" : model.vaultPath)
                    .foregroundStyle(model.vaultPath.isEmpty ? .secondary : .primary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 5)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6))
                Button("Change…") { model.chooseFolder() }
                    .disabled(!model.canChangeFolder)
            }
            if let hint = folderHint {
                Text(hint.text)
                    .font(.caption)
                    .foregroundStyle(hint.isProblem ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
            }
            Button("Reveal in Finder") { model.revealVault() }
                .disabled(!model.isConfigured)
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
