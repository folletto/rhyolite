import SwiftUI

/// The Log tab: the CLI's output filling the tab, with Copy Log below it.
struct LogTab: View {
    let model: AppModel

    var body: some View {
        VStack(spacing: 12) {
            LogTextView(log: model.server.log)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .overlay {
                    if model.server.log.lines.isEmpty {
                        Text("Output from rhyolite appears here once it starts.")
                            .foregroundStyle(.secondary)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: 6))
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(.quaternary))
            HStack {
                Spacer()
                Button("Copy Log") { model.copyLog() }
                    .disabled(model.server.log.lines.isEmpty)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}
