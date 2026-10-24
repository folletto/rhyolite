import XCTest
@testable import RhyoliteCore

final class ClientListTests: XCTestCase {
    /// 2026-09-30T12:00:00Z, the instant `stamp(0)` renders.
    private let t0 = Date(timeIntervalSince1970: 1_790_769_600)

    /// The UTC time `seconds` after 12:00:00 on 2026-09-30, as the CLI writes it.
    private func stamp(_ seconds: Int) -> String {
        String(format: "2026-09-30T12:%02ld:%02ldZ", seconds / 60, seconds % 60)
    }

    private func initLine(_ ts: String, name: String = "Claude", ip: String = "10.0.0.1",
                          session: String? = nil, version: String = "1.0") -> String {
        var line = "ts=\(ts) event=client_init ip=\(ip) name=\"\(name)\" version=\(version)"
        if let session { line += " session=\(session)" }
        return line
    }

    private func httpLine(_ ts: String, status: Int = 200, session: String? = nil) -> String {
        var line = "ts=\(ts) event=http method=POST path=/mcp status=\(status) dur_ms=3 ip=10.0.0.1"
        if let session { line += " session=\(session)" }
        return line
    }

    private func authLine(_ ts: String, ip: String) -> String {
        "ts=\(ts) event=auth_fail method=POST path=/mcp ip=\(ip)"
    }

    // MARK: - Logfmt

    func testParsesBareAndQuotedValues() throws {
        let f = try XCTUnwrap(Logfmt.parse(#"ts=2026-09-30T12:00:00Z event=client_init ip=127.0.0.1 name="Claude Desktop" note="a=b" version=1.2"#))
        XCTAssertEqual(f["ts"], "2026-09-30T12:00:00Z")
        XCTAssertEqual(f["event"], "client_init")
        XCTAssertEqual(f["ip"], "127.0.0.1")
        XCTAssertEqual(f["name"], "Claude Desktop")
        XCTAssertEqual(f["note"], "a=b")
        XCTAssertEqual(f["version"], "1.2")
    }

    func testDecodesEscapesInQuotedValues() throws {
        let f = try XCTUnwrap(Logfmt.parse(#"ts=1 a="say \"hi\" \\ tab\there" b="x\ny\rz" c="\x41é\U0001F600" d="\q" e="\a\b\f\v""#))
        XCTAssertEqual(f["a"], "say \"hi\" \\ tab\there")
        XCTAssertEqual(f["b"], "x\ny\rz")
        XCTAssertEqual(f["c"], "Aé😀")
        XCTAssertEqual(f["d"], "q")
        XCTAssertEqual(f["e"], "\u{07}\u{08}\u{0C}\u{0B}")
    }

    func testMalformedEscapesKeepTheCharacterAfterTheBackslash() throws {
        let f = try XCTUnwrap(Logfmt.parse(#"ts=1 a="\xZZ" b="\u12" c="\ud800" d="\U00110000""#))
        XCTAssertEqual(f["a"], "xZZ")
        XCTAssertEqual(f["b"], "u12")
        XCTAssertEqual(f["c"], "\u{FFFD}")
        XCTAssertEqual(f["d"], "\u{FFFD}")
    }

    func testEmptyQuotedValueIsEmpty() throws {
        let f = try XCTUnwrap(Logfmt.parse(#"ts=1 name="" ip=10.0.0.1"#))
        XCTAssertEqual(f["name"], "")
        XCTAssertEqual(f["ip"], "10.0.0.1")
    }

    func testUnterminatedQuoteTakesTheRestOfTheLine() throws {
        let f = try XCTUnwrap(Logfmt.parse(#"ts=1 msg="abc def ip=10.0.0.1"#))
        XCTAssertEqual(f["msg"], "abc def ip=10.0.0.1")
        XCTAssertNil(f["ip"])
    }

    func testBareWordsWithoutAValueAreSkipped() throws {
        let f = try XCTUnwrap(Logfmt.parse("ts=1 flag ip=10.0.0.1 =x"))
        XCTAssertNil(f["flag"])
        XCTAssertEqual(f["ip"], "10.0.0.1")
        XCTAssertEqual(f.count, 2)
    }

    func testLinesWithoutTheTsPrefixAreNotLogfmt() {
        XCTAssertNil(Logfmt.parse("▲ Rhyolite"))
        XCTAssertNil(Logfmt.parse("  on: http://127.0.0.1:8080/mcp"))
        XCTAssertNil(Logfmt.parse(""))
        XCTAssertNil(Logfmt.parse("x ts=1 event=http"))
    }

    func testMalformedInputNeverCrashes() {
        let lines = ["ts=", "ts=1 =", "ts=1 ==", "ts=1 \"", "ts=1 a=\"", "ts=1 a=\"\\", "ts=1 a=\"\\x4",
                     "ts=1 a=\"\\u00", "ts=1 a=\"\\\"", "ts=1 a=\"\\U1234567"]
        for line in lines {
            XCTAssertNotNil(Logfmt.parse(line), line)
        }
    }

    // MARK: - Client list

    func testClientInitCreatesARow() {
        var list = ClientList()
        list.consume([initLine(stamp(0), name: "Claude", ip: "10.0.0.1", session: "s1", version: "1.2")])
        XCTAssertEqual(list.clients.count, 1)
        let c = list.clients[0]
        XCTAssertEqual(c.id, "app|Claude|10.0.0.1")
        XCTAssertEqual(c.ip, "10.0.0.1")
        XCTAssertEqual(c.name, "Claude")
        XCTAssertEqual(c.version, "1.2")
        XCTAssertEqual(c.firstSeen, t0)
        XCTAssertEqual(c.lastSeen, t0)
        XCTAssertEqual(c.failedAttempts, 0)
        XCTAssertFalse(c.isFailed)
    }

    func testRepeatInitUpdatesLastSeenNotARow() {
        var list = ClientList()
        list.consume([initLine(stamp(0)), initLine(stamp(90), version: "1.3")])
        XCTAssertEqual(list.clients.count, 1)
        XCTAssertEqual(list.clients[0].firstSeen, t0)
        XCTAssertEqual(list.clients[0].lastSeen, t0.addingTimeInterval(90))
        XCTAssertEqual(list.clients[0].version, "1.3")

        // An older line never moves lastSeen back.
        list.consume([initLine(stamp(10))])
        XCTAssertEqual(list.clients.count, 1)
        XCTAssertEqual(list.clients[0].lastSeen, t0.addingTimeInterval(90))
    }

    func testSameNameOnDifferentIPsAreSeparateRows() {
        var list = ClientList()
        list.consume([initLine(stamp(0), ip: "10.0.0.1"), initLine(stamp(1), ip: "10.0.0.2")])
        XCTAssertEqual(list.clients.map(\.id), ["app|Claude|10.0.0.2", "app|Claude|10.0.0.1"])
    }

    func testDifferentNamesOnOneIPAreSeparateRows() {
        var list = ClientList()
        list.consume([initLine(stamp(0), name: "Claude"), initLine(stamp(1), name: "Cursor")])
        XCTAssertEqual(list.clients.map(\.name), ["Cursor", "Claude"])
    }

    func testRequestUpdatesOnlyItsOwnRow() throws {
        var list = ClientList()
        list.consume([
            initLine(stamp(0), name: "A", ip: "10.0.0.1", session: "s1"),
            initLine(stamp(1), name: "B", ip: "10.0.0.2", session: "s2"),
            httpLine(stamp(5), session: "s1"),
        ])
        let a = try XCTUnwrap(list.clients.first(where: { $0.name == "A" }))
        let b = try XCTUnwrap(list.clients.first(where: { $0.name == "B" }))
        XCTAssertEqual(a.firstSeen, t0)
        XCTAssertEqual(a.lastSeen, t0.addingTimeInterval(5))
        XCTAssertEqual(b.lastSeen, t0.addingTimeInterval(1))
    }

    func testRequestWithoutAKnownSessionIsIgnored() {
        var list = ClientList()
        list.consume([initLine(stamp(0), session: "s1")])
        let before = list.clients
        list.consume([httpLine(stamp(9)), httpLine(stamp(9), session: "unknown")])
        XCTAssertEqual(list.clients, before)
    }

    func testUnauthorizedRequestsAreIgnored() {
        var list = ClientList()
        list.consume([initLine(stamp(0), session: "s1")])
        let before = list.clients
        list.consume([httpLine(stamp(9), status: 401, session: "s1")])
        XCTAssertEqual(list.clients, before)
    }

    func testAuthFailsGroupPerIPAndCountAttempts() throws {
        var list = ClientList()
        list.consume([
            authLine(stamp(0), ip: "203.0.113.5"),
            authLine(stamp(1), ip: "203.0.113.5"),
            authLine(stamp(2), ip: "203.0.113.9"),
        ])
        XCTAssertEqual(list.clients.count, 2)
        let repeated = try XCTUnwrap(list.clients.first(where: { $0.ip == "203.0.113.5" }))
        XCTAssertEqual(repeated.id, "fail|203.0.113.5")
        XCTAssertNil(repeated.name)
        XCTAssertTrue(repeated.isFailed)
        XCTAssertEqual(repeated.failedAttempts, 2)
        XCTAssertEqual(repeated.firstSeen, t0)
        XCTAssertEqual(repeated.lastSeen, t0.addingTimeInterval(1))
        XCTAssertEqual(list.clients.first(where: { $0.ip == "203.0.113.9" })?.failedAttempts, 1)
    }

    func testNewestFirst() {
        var list = ClientList()
        list.consume([
            initLine(stamp(0), name: "A", ip: "10.0.0.1", session: "a"),
            initLine(stamp(1), name: "B", ip: "10.0.0.2"),
            initLine(stamp(2), name: "C", ip: "10.0.0.3"),
        ])
        XCTAssertEqual(list.clients.map(\.name), ["C", "B", "A"])

        list.consume([httpLine(stamp(3), session: "a")])
        XCTAssertEqual(list.clients.map(\.name), ["A", "C", "B"])
    }

    func testEqualTimesKeepInsertionOrder() {
        var list = ClientList()
        list.consume([
            initLine(stamp(0), name: "First", ip: "10.0.0.1"),
            initLine(stamp(0), name: "Second", ip: "10.0.0.2"),
        ])
        XCTAssertEqual(list.clients.map(\.name), ["First", "Second"])
    }

    func testTimestampsWithZOffsetAndFractionsParse() throws {
        var list = ClientList()
        list.consume([
            initLine("2026-09-30T12:00:00Z", name: "UTC", ip: "10.0.0.1"),
            initLine("2026-09-30T14:00:00+02:00", name: "Offset", ip: "10.0.0.2"),
            initLine("2026-09-30T12:00:00.25Z", name: "Fraction", ip: "10.0.0.3"),
        ])
        let utc = try XCTUnwrap(list.clients.first(where: { $0.name == "UTC" }))
        let offset = try XCTUnwrap(list.clients.first(where: { $0.name == "Offset" }))
        let fraction = try XCTUnwrap(list.clients.first(where: { $0.name == "Fraction" }))
        XCTAssertEqual(utc.firstSeen, t0)
        XCTAssertEqual(offset.firstSeen, t0)
        XCTAssertEqual(fraction.firstSeen.timeIntervalSince(t0), 0.25, accuracy: 0.001)
    }

    func testLinesWithAnUnparsableTimestampAreSkipped() {
        var list = ClientList()
        list.consume([
            "ts=yesterday event=client_init ip=10.0.0.1 name=\"Claude\"",
            "ts= event=auth_fail ip=10.0.0.1",
        ])
        XCTAssertTrue(list.clients.isEmpty)
    }

    func testCapDropsTheOldestClientWhichReturnsOnItsNextRequest() throws {
        var list = ClientList()
        let total = ClientList.maxClients + 5
        list.consume((0..<total).map { initLine(stamp($0), name: "App", ip: "10.0.0.\($0)", session: "s\($0)") })
        XCTAssertEqual(list.clients.count, ClientList.maxClients)
        XCTAssertEqual(list.clients.first?.ip, "10.0.0.\(total - 1)")
        XCTAssertEqual(list.clients.last?.ip, "10.0.0.5")

        // The dropped client's session is still known, so its next request brings the row back as the
        // newest, pushing out the oldest row in turn.
        list.consume([httpLine(stamp(total), session: "s0")])
        XCTAssertEqual(list.clients.count, ClientList.maxClients)
        let back = try XCTUnwrap(list.clients.first)
        XCTAssertEqual(back.id, "app|App|10.0.0.0")
        XCTAssertEqual(back.firstSeen, t0.addingTimeInterval(Double(total)))
        XCTAssertEqual(back.lastSeen, back.firstSeen)
        XCTAssertNil(list.clients.first(where: { $0.ip == "10.0.0.5" }))
    }

    func testClearEmptiesClientsButKeepsSessions() throws {
        var list = ClientList()
        list.consume([initLine(stamp(0), session: "s1"), authLine(stamp(1), ip: "203.0.113.5")])
        list.clear()
        XCTAssertTrue(list.clients.isEmpty)

        // The session outlived the clear, so its next request brings the app back, first seen then.
        list.consume([httpLine(stamp(2), session: "s1")])
        XCTAssertEqual(list.clients.count, 1)
        let app = try XCTUnwrap(list.clients.first)
        XCTAssertEqual(app.id, "app|Claude|10.0.0.1")
        XCTAssertEqual(app.version, "1.0")
        XCTAssertEqual(app.firstSeen, t0.addingTimeInterval(2))
        XCTAssertEqual(app.lastSeen, t0.addingTimeInterval(2))
        XCTAssertEqual(app.failedAttempts, 0)
    }

    func testUnauthorizedRequestAfterClearDoesNotBringTheAppBack() {
        var list = ClientList()
        list.consume([initLine(stamp(0), session: "s1")])
        list.clear()
        list.consume([httpLine(stamp(1), status: 401, session: "s1")])
        XCTAssertTrue(list.clients.isEmpty)
    }

    func testSessionsOfAppsWithNoRowArePrunedPastTheThreshold() throws {
        var list = ClientList()
        // One app per line. The cap drops the oldest rows while their sessions pile up past the threshold.
        let total = 1_005
        list.consume((0..<total).map { initLine(stamp($0), name: "App\($0)", session: "s\($0)") })
        XCTAssertEqual(list.clients.count, ClientList.maxClients)

        // The pruned session cannot bring its app back.
        list.consume([httpLine(stamp(total), session: "s0")])
        XCTAssertEqual(list.clients.count, ClientList.maxClients)
        XCTAssertNil(list.clients.first(where: { $0.name == "App0" }))

        // A session whose app still has a row keeps working.
        list.consume([httpLine(stamp(total + 1), session: "s\(total - 1)")])
        let newest = try XCTUnwrap(list.clients.first)
        XCTAssertEqual(newest.name, "App\(total - 1)")
        XCTAssertEqual(newest.lastSeen, t0.addingTimeInterval(Double(total + 1)))
    }

    func testBannerAndUnknownEventsAreIgnored() {
        var list = ClientList()
        list.consume([
            "▲ Rhyolite",
            "  on: http://192.168.1.5:8080/mcp",
            "● Listening",
            "2026-09-30 12:00:00 event=client_init ip=10.0.0.1 name=\"Claude\"",
            "ts=\(stamp(0)) event=startup port=8080",
            "",
        ])
        XCTAssertTrue(list.clients.isEmpty)
    }
}
