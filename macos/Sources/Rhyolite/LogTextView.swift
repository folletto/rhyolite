import AppKit
import RhyoliteCore
import SwiftUI

/// Read-only, selectable, monospaced text showing the CLI's output. It starts scrolled to the end
/// and follows new lines while the reader is at the end. Otherwise it leaves the scroll position alone.
struct LogTextView: NSViewRepresentable {
    /// The app's log. Its counts tell the view what changed since it last updated.
    let log: LogBuffer

    /// What the text view currently shows, so an update can append only the new lines.
    final class Coordinator {
        /// The log revision last shown. An unchanged revision means nothing to do.
        var revision = -1
        /// The log's `totalAppended` last shown: how many lines the view has taken in.
        var shownTotal = 0
        /// The log's `clearCount` last shown. Stays -1 until the first update.
        var clearCount = -1
        /// How many lines the text storage holds. It can keep lines the buffer has trimmed, until it is rebuilt.
        var storedLines = 0
    }

    /// How many lines the text storage may hold beyond the buffer's before it is rebuilt from the buffer.
    private static let maxExtraLines = 1000

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSTextView.scrollableTextView()
        scroll.borderType = .noBorder
        let textView = scroll.documentView as! NSTextView
        textView.isEditable = false
        textView.isSelectable = true
        textView.isRichText = false
        textView.font = Self.font
        textView.textColor = .labelColor
        textView.backgroundColor = .textBackgroundColor
        textView.textContainerInset = NSSize(width: 6, height: 6)
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        let shown = context.coordinator
        guard shown.revision != log.revision,
              let textView = scroll.documentView as? NSTextView,
              let storage = textView.textStorage else { return }
        shown.revision = log.revision

        // Measured before the text changes, while the text view's height still belongs to the old text.
        // The first update always starts at the end.
        let firstUpdate = shown.clearCount < 0
        let atEnd = firstUpdate || scroll.contentView.bounds.maxY >= textView.frame.height - 24
        let origin = scroll.contentView.bounds.origin

        // Append only when the buffer still holds every new line, and the storage has not grown far past
        // the buffer's trimmed lines. Otherwise rebuild the text from the buffer.
        let newLines = log.totalAppended - shown.shownTotal
        let storedAfter = shown.storedLines + newLines
        let canAppend = shown.clearCount == log.clearCount
            && newLines >= 1 && newLines <= log.lines.count
            && storedAfter - log.lines.count <= Self.maxExtraLines

        if canAppend {
            // Lines are joined with a line break, so the first new line needs one only when lines already exist.
            let separator = shown.storedLines > 0 ? "\n" : ""
            storage.append(Self.attributed(separator + log.lines.suffix(newLines).joined(separator: "\n")))
            shown.storedLines = storedAfter
        } else {
            storage.setAttributedString(Self.attributed(log.text))
            shown.storedLines = log.lines.count
        }
        shown.shownTotal = log.totalAppended
        shown.clearCount = log.clearCount

        if atEnd {
            Self.scrollToEnd(textView)
        } else if !canAppend {
            // A rebuild can move the text under the reader, so put the scroll position back.
            Self.restore(origin, in: scroll)
        }
    }

    private static var font: NSFont { .monospacedSystemFont(ofSize: 12, weight: .regular) }

    private static func attributed(_ text: String) -> NSAttributedString {
        NSAttributedString(string: text, attributes: [.font: font, .foregroundColor: NSColor.labelColor])
    }

    /// Scrolls to the end once the change has been laid out.
    private static func scrollToEnd(_ textView: NSTextView) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated { textView.scrollToEndOfDocument(nil) }
        }
    }

    /// Restores a scroll origin, clamped to the new height, once the change has been laid out.
    private static func restore(_ origin: NSPoint, in scroll: NSScrollView) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                guard let document = scroll.documentView else { return }
                let maxY = max(0, document.frame.height - scroll.contentView.bounds.height)
                scroll.contentView.scroll(to: NSPoint(x: origin.x, y: min(origin.y, maxY)))
                scroll.reflectScrolledClipView(scroll.contentView)
            }
        }
    }
}
