import RhyoliteCore
import SwiftUI

/// The Clients tab: every app that used the server since the app launched, newest first, one compact row each.
/// Failed attempts are grouped per IP, since a refused request never says which app sent it.
struct ClientsTab: View {
    let model: AppModel

    var body: some View {
        VStack(spacing: 12) {
            Form {
                Section {
                    if model.clients.clients.isEmpty {
                        Text("No clients have connected yet.")
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(model.clients.clients) { client in
                            row(client)
                        }
                    }
                } footer: {
                    if model.clients.clients.contains(where: \.isFailed) {
                        Text("Failed attempts used a wrong or missing token. Check the token in that client's mcp.json.")
                    }
                }
            }
            .formStyle(.grouped)
            HStack {
                Spacer()
                Button("Clear List") { model.clearClients() }
                    .disabled(model.clients.clients.isEmpty)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    /// One client: icon, name and IP on the left; last connection, then first connection, on the right.
    private func row(_ client: Client) -> some View {
        HStack(spacing: 12) {
            Image(systemName: client.isFailed ? "exclamationmark.triangle.fill" : "laptopcomputer")
                .font(.title2)
                .foregroundStyle(client.isFailed ? AnyShapeStyle(.red) : AnyShapeStyle(.tint))
                .frame(width: 28)
            VStack(alignment: .leading, spacing: 4) {
                Text(title(of: client))
                    .font(.body)
                Text(subtitle(of: client))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text(client.lastSeen.formatted(date: .abbreviated, time: .shortened))
                    .font(.subheadline)
                    .help(client.isFailed ? "Last attempt" : "Last connection")
                Text(client.firstSeen.formatted(date: .abbreviated, time: .shortened))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .help(client.isFailed ? "First attempt" : "First connection")
            }
        }
    }

    /// The IP, plus the attempt count for failed attempts.
    private func subtitle(of client: Client) -> String {
        guard client.isFailed else { return client.ip }
        let n = client.failedAttempts
        return "\(client.ip) · \(n) failed \(n == 1 ? "attempt" : "attempts")"
    }

    /// The app's name; failed attempts and apps that sent no name get a stand-in.
    private func title(of client: Client) -> String {
        guard let name = client.name else { return "Unknown app" }
        return name.isEmpty ? "Unnamed app" : name
    }
}
