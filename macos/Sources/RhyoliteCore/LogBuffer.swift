import Foundation

/// Turns arbitrary byte chunks into complete lines. A trailing partial line is
/// held back until its newline arrives, so a multi-byte character such as "▴"
/// split across two reads is never decoded half-way.
public struct LineSplitter {
    private var pending = Data()

    public init() {}

    public mutating func feed(_ data: Data) -> [String] {
        pending.append(data)
        var lines: [String] = []
        while let nl = pending.firstIndex(of: 0x0A) {
            lines.append(Self.decode(pending[pending.startIndex..<nl]))
            pending.removeSubrange(pending.startIndex...nl)
        }
        return lines
    }

    /// Returns the unterminated tail, if any. Call when the stream ends.
    public mutating func flush() -> String? {
        guard !pending.isEmpty else { return nil }
        defer { pending.removeAll() }
        return Self.decode(pending)
    }

    private static func decode(_ bytes: some DataProtocol) -> String {
        var s = String(decoding: Data(bytes), as: UTF8.self)
        if s.hasSuffix("\r") { s.removeLast() }
        return s
    }
}

/// The most recent output lines, bounded so a long session cannot grow without limit.
public struct LogBuffer {
    public static let defaultMaxLines = 5000

    public private(set) var lines: [String] = []
    /// Bumped on every change, so views can tell cheaply that something changed.
    public private(set) var revision = 0
    private let maxLines: Int

    public init(maxLines: Int = LogBuffer.defaultMaxLines) {
        self.maxLines = maxLines
    }

    public mutating func append(_ newLines: [String]) {
        guard !newLines.isEmpty else { return }
        lines.append(contentsOf: newLines)
        if lines.count > maxLines {
            lines.removeFirst(lines.count - maxLines)
        }
        revision += 1
    }

    public mutating func clear() {
        lines.removeAll()
        revision += 1
    }

    public var text: String { lines.joined(separator: "\n") }
}
