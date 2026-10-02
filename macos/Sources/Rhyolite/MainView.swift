import RhyoliteCore
import SwiftUI

struct MainView: View {
    let model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            folderRow
            controlRow
            logView
        }
        .padding(16)
        .frame(minWidth: 520, minHeight: 360)
    }

    // MARK: Folder

    private var folderRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Vault folder")
                    .frame(width: 84, alignment: .leading)
                Text(model.vaultPath.isEmpty ? "No folder chosen" : model.vaultPath)
                    .foregroundStyle(model.vaultPath.isEmpty ? .secondary : .primary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 5)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6))
                Button("Choose…") { model.chooseFolder() }
                    .disabled(!model.canChangeFolder)
            }
            if let hint = folderHint {
                Text(hint.text)
                    .font(.caption)
                    .foregroundStyle(hint.isProblem ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
                    .padding(.leading, 92)
            }
        }
    }

    private var folderHint: (text: String, isProblem: Bool)? {
        if model.vaultPath.isEmpty { return ("Choose the folder to serve before starting.", false) }
        if !model.isConfigured { return ("This folder no longer exists. Choose another one.", true) }
        if model.server.isActive { return ("Stop the server to change the folder.", false) }
        return nil
    }

    // MARK: Start / stop

    private var controlRow: some View {
        HStack(spacing: 8) {
            Circle().fill(statusColor).frame(width: 10, height: 10)
            Text(statusText)
            Spacer()
            Button("Reveal in Finder") { model.revealVault() }
                .disabled(!model.isConfigured)
            Button("Copy Log") { model.copyLog() }
                .disabled(model.server.log.lines.isEmpty)
            Button(buttonTitle) {
                if model.server.state == .starting || model.server.state == .running {
                    model.stop()
                } else {
                    model.start()
                }
            }
            .keyboardShortcut(.defaultAction)
            .disabled(!buttonEnabled)
            .frame(minWidth: 80)
        }
    }

    private var buttonTitle: String {
        switch model.server.state {
        case .stopped, .failed: "Start"
        case .starting, .running: "Stop"
        case .stopping: "Stopping…"
        }
    }

    private var buttonEnabled: Bool {
        switch model.server.state {
        case .stopped, .failed: model.isConfigured
        case .starting, .running: true
        case .stopping: false
        }
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

    // MARK: Log

    private var logView: some View {
        LogTextView(text: model.server.log.text, revision: model.server.log.revision)
            .overlay {
                if model.server.log.lines.isEmpty {
                    Text("Output from rhyolite appears here once it starts.")
                        .foregroundStyle(.secondary)
                }
            }
            .clipShape(RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(.quaternary))
    }
}

/// Read-only, selectable, monospaced text that follows new output while the
/// user is at the bottom and leaves the scroll position alone otherwise.
struct LogTextView: NSViewRepresentable {
    let text: String
    let revision: Int

    final class Coordinator {
        var revision = -1
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSTextView.scrollableTextView()
        scroll.borderType = .noBorder
        let textView = scroll.documentView as! NSTextView
        textView.isEditable = false
        textView.isSelectable = true
        textView.isRichText = false
        textView.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        textView.textColor = .labelColor
        textView.backgroundColor = .textBackgroundColor
        textView.textContainerInset = NSSize(width: 6, height: 6)
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        guard context.coordinator.revision != revision,
              let textView = scroll.documentView as? NSTextView else { return }
        context.coordinator.revision = revision
        let atBottom = scroll.contentView.bounds.maxY >= textView.frame.height - 24
        textView.string = text
        if atBottom { textView.scrollToEndOfDocument(nil) }
    }
}
