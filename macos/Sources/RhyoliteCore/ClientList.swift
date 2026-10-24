import Foundation

private let space = UInt8(ascii: " ")
private let equals = UInt8(ascii: "=")
private let quote = UInt8(ascii: "\"")
private let backslash = UInt8(ascii: "\\")

/// The logfmt the CLI writes: space-separated `key=value` pairs. A value that is empty, or holds a
/// space, `=`, `"` or a control character, is double-quoted with the escapes of Go's `strconv.Quote`.
enum Logfmt {
    /// The fields of a line, or nil unless it starts with `ts=`, which keeps banner text out.
    /// Never fails on malformed input: an unterminated quote runs to the end of the line.
    static func parse(_ line: String) -> [String: String]? {
        guard line.hasPrefix("ts=") else { return nil }
        let bytes = Array(line.utf8)
        var fields: [String: String] = [:]
        var i = 0
        while i < bytes.count {
            guard bytes[i] != space else {
                i += 1
                continue
            }
            let keyStart = i
            while i < bytes.count, bytes[i] != equals, bytes[i] != space { i += 1 }
            // A word with no `=` has no value, so it is skipped.
            guard i < bytes.count, bytes[i] == equals else { continue }
            let key = String(decoding: bytes[keyStart..<i], as: UTF8.self)
            i += 1

            let value: [UInt8]
            if i < bytes.count, bytes[i] == quote {
                let quoted = unquote(bytes, from: i + 1)
                value = quoted.value
                i = quoted.next
            } else {
                // Bare values have no escapes, so a backslash in one is kept as it is.
                let start = i
                while i < bytes.count, bytes[i] != space { i += 1 }
                value = Array(bytes[start..<i])
            }
            if !key.isEmpty { fields[key] = String(decoding: value, as: UTF8.self) }
        }
        return fields
    }

    /// Decodes the quoted value whose opening quote is just before `start`. Returns the decoded bytes
    /// and the index just past the closing quote (the end of the line if there is none).
    private static func unquote(_ bytes: [UInt8], from start: Int) -> (value: [UInt8], next: Int) {
        var out: [UInt8] = []
        var i = start
        while i < bytes.count {
            let byte = bytes[i]
            if byte == quote { return (out, i + 1) }
            guard byte == backslash else {
                out.append(byte)
                i += 1
                continue
            }
            guard i + 1 < bytes.count else { break } // A lone backslash at the end of the line is dropped.
            let escaped = bytes[i + 1]
            i += 2
            switch escaped {
            case UInt8(ascii: "a"): out.append(0x07)
            case UInt8(ascii: "b"): out.append(0x08)
            case UInt8(ascii: "f"): out.append(0x0C)
            case UInt8(ascii: "n"): out.append(0x0A)
            case UInt8(ascii: "r"): out.append(0x0D)
            case UInt8(ascii: "t"): out.append(0x09)
            case UInt8(ascii: "v"): out.append(0x0B)
            case UInt8(ascii: "x"):
                // One byte, as in Go. A byte sequence that is not valid UTF-8 decodes to U+FFFD.
                if let v = hexValue(bytes, at: i, digits: 2) {
                    out.append(UInt8(v))
                    i += 2
                } else {
                    out.append(escaped)
                }
            case UInt8(ascii: "u"), UInt8(ascii: "U"):
                let digits = escaped == UInt8(ascii: "u") ? 4 : 8
                if let v = hexValue(bytes, at: i, digits: digits) {
                    // Surrogates and values past U+10FFFF are not scalars, so they become U+FFFD.
                    let scalar = Unicode.Scalar(v) ?? "\u{FFFD}"
                    out.append(contentsOf: String(Character(scalar)).utf8)
                    i += digits
                } else {
                    out.append(escaped)
                }
            default:
                // `\"`, `\\` and unknown escapes all keep the character after the backslash.
                out.append(escaped)
            }
        }
        return (out, bytes.count)
    }

    /// The value of the `digits` hex digits at `start`, or nil if there are too few or one is not hex.
    private static func hexValue(_ bytes: [UInt8], at start: Int, digits: Int) -> UInt32? {
        guard start + digits <= bytes.count else { return nil }
        var value: UInt32 = 0
        for byte in bytes[start..<start + digits] {
            guard let nibble = hexNibble(byte) else { return nil }
            value = value << 4 | UInt32(nibble)
        }
        return value
    }

    private static func hexNibble(_ byte: UInt8) -> UInt8? {
        switch byte {
        case UInt8(ascii: "0")...UInt8(ascii: "9"): return byte - UInt8(ascii: "0")
        case UInt8(ascii: "a")...UInt8(ascii: "f"): return byte - UInt8(ascii: "a") + 10
        case UInt8(ascii: "A")...UInt8(ascii: "F"): return byte - UInt8(ascii: "A") + 10
        default: return nil
        }
    }
}

/// One app as the server sees it. A client is one app name at one IP. Failed attempts are grouped
/// per IP instead, since a refused request never reveals which app sent it, so those rows have no name.
public struct Client: Identifiable, Equatable, Sendable {
    /// `app|<name>|<ip>`, or `fail|<ip>` for failed attempts.
    public let id: String
    public let ip: String
    /// The name the app reported; nil for a row of failed attempts.
    public let name: String?
    /// The version the app reported, or empty if it did not say.
    public internal(set) var version: String
    public internal(set) var firstSeen: Date
    public internal(set) var lastSeen: Date
    /// Refused requests from this IP. Zero for an app row.
    public internal(set) var failedAttempts: Int

    public var isFailed: Bool { name == nil }
}

/// The clients that have talked to the server, newest first. Feed it the CLI's output lines as they
/// arrive; lines that are not log events are ignored.
public struct ClientList: Equatable, Sendable {
    /// The most rows kept. Beyond this, the least recently seen are dropped.
    public static let maxClients = 200
    /// Past this many sessions, the sessions of apps with no row are pruned. See `sortAndTrim()`.
    private static let pruneSessionsAbove = 1000

    /// The app that opened a session, so its row can come back after the list is cleared.
    private struct SessionOwner: Equatable, Sendable {
        let name: String
        let ip: String
        let version: String
    }

    /// Newest first, by `lastSeen`.
    public private(set) var clients: [Client] = []
    /// Session id to the app that opened it. Sessions outlive their rows, so a request on one brings its
    /// app back after the list was cleared or trimmed.
    private var sessions: [String: SessionOwner] = [:]

    public init() {}

    /// Applies a batch of output lines. A line is used only if it is logfmt with a valid `ts`.
    public mutating func consume(_ lines: [String]) {
        for line in lines {
            guard let fields = Logfmt.parse(line),
                  let text = fields["ts"],
                  let ts = Self.timestamp(text) else { continue }
            switch fields["event"] ?? "" {
            case "client_init": clientInit(fields, at: ts)
            case "http": request(fields, at: ts)
            case "auth_fail": authFailed(fields, at: ts)
            default: break
            }
        }
        sortAndTrim()
    }

    /// Forgets every client. Apps with an open session come back on their next request.
    public mutating func clear() {
        clients.removeAll()
    }

    /// An app announced itself: a new row, or another sighting of a known one.
    private mutating func clientInit(_ fields: [String: String], at ts: Date) {
        guard let ip = fields["ip"], !ip.isEmpty else { return }
        let name = fields["name"] ?? ""
        let id = Self.appID(name: name, ip: ip)
        if let i = clients.firstIndex(where: { $0.id == id }) {
            clients[i].lastSeen = max(clients[i].lastSeen, ts)
            if let version = fields["version"] { clients[i].version = version }
        } else {
            clients.append(Client(id: id, ip: ip, name: name, version: fields["version"] ?? "",
                                  firstSeen: ts, lastSeen: ts, failedAttempts: 0))
        }
        if let session = fields["session"], !session.isEmpty {
            sessions[session] = SessionOwner(name: name, ip: ip, version: fields["version"] ?? "")
        }
    }

    /// A request in a known session counts as activity for its app. If the app's row was cleared or
    /// trimmed, the request brings it back. Unauthorized requests never do.
    private mutating func request(_ fields: [String: String], at ts: Date) {
        guard fields["status"] != "401",
              let session = fields["session"],
              let owner = sessions[session] else { return }
        let id = Self.appID(name: owner.name, ip: owner.ip)
        if let i = clients.firstIndex(where: { $0.id == id }) {
            clients[i].lastSeen = max(clients[i].lastSeen, ts)
        } else {
            clients.append(Client(id: id, ip: owner.ip, name: owner.name, version: owner.version,
                                  firstSeen: ts, lastSeen: ts, failedAttempts: 0))
        }
    }

    /// A refused request, counted against its IP rather than an app.
    private mutating func authFailed(_ fields: [String: String], at ts: Date) {
        guard let ip = fields["ip"], !ip.isEmpty else { return }
        let id = "fail|\(ip)"
        if let i = clients.firstIndex(where: { $0.id == id }) {
            clients[i].failedAttempts += 1
            clients[i].lastSeen = max(clients[i].lastSeen, ts)
        } else {
            clients.append(Client(id: id, ip: ip, name: nil, version: "",
                                  firstSeen: ts, lastSeen: ts, failedAttempts: 1))
        }
    }

    /// Sorts newest first, then drops the least recently seen beyond `maxClients`. Equal times keep
    /// their current order, so rows that were inserted earlier stay ahead.
    private mutating func sortAndTrim() {
        clients = clients.enumerated().sorted { a, b in
            if a.element.lastSeen != b.element.lastSeen { return a.element.lastSeen > b.element.lastSeen }
            return a.offset < b.offset
        }.map(\.element)
        if clients.count > Self.maxClients {
            clients.removeSubrange(Self.maxClients...)
        }
        // Sessions outlive their rows, so a dropped app comes back on its next request. To bound memory,
        // the sessions of apps with no row are pruned once there are more than `pruneSessionsAbove`. Such
        // an app then needs a new session before it reappears.
        guard sessions.count > Self.pruneSessionsAbove else { return }
        let rows = Set(clients.map(\.id))
        sessions = sessions.filter { rows.contains(Self.appID(name: $0.value.name, ip: $0.value.ip)) }
    }

    /// The row id of an app: `app|<name>|<ip>`.
    private static func appID(name: String, ip: String) -> String {
        "app|\(name)|\(ip)"
    }

    /// RFC 3339 as the CLI writes it (`Z` or an offset). Fractional seconds are accepted too.
    private static func timestamp(_ text: String) -> Date? {
        try? Date.ISO8601FormatStyle(includingFractionalSeconds: true).parse(text)
    }
}
