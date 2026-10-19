import XCTest
@testable import RhyoliteCore

final class LogBufferTests: XCTestCase {
    func testSplitsLinesAcrossChunks() {
        var s = LineSplitter()
        XCTAssertEqual(s.feed(Data("one\ntw".utf8)), ["one"])
        XCTAssertEqual(s.feed(Data("o\nthree\n".utf8)), ["two", "three"])
        XCTAssertNil(s.flush())
    }

    func testPartialLineHeldUntilFlush() {
        var s = LineSplitter()
        XCTAssertEqual(s.feed(Data("no newline".utf8)), [])
        XCTAssertEqual(s.flush(), "no newline")
        XCTAssertNil(s.flush())
    }

    func testMultiByteCharacterSplitAcrossChunks() {
        let bytes = Array("▴ Listening\n".utf8) // ▴ is 3 bytes
        var s = LineSplitter()
        XCTAssertEqual(s.feed(Data(bytes[0..<1])), [])
        XCTAssertEqual(s.feed(Data(bytes[1..<2])), [])
        XCTAssertEqual(s.feed(Data(bytes[2...])), ["▴ Listening"])
    }

    func testCRLFAndEmptyLines() {
        var s = LineSplitter()
        XCTAssertEqual(s.feed(Data("a\r\n\nb\n".utf8)), ["a", "", "b"])
    }

    func testBufferKeepsOnlyTheLastLines() {
        var b = LogBuffer(maxLines: 3)
        b.append(["1", "2"])
        b.append(["3", "4", "5"])
        XCTAssertEqual(b.lines, ["3", "4", "5"])
    }

    func testTotalAppendedCountsLinesEvenWhenTrimmed() {
        var b = LogBuffer(maxLines: 3)
        b.append(["1", "2"])
        XCTAssertEqual(b.totalAppended, 2)
        b.append([])
        XCTAssertEqual(b.totalAppended, 2)
        b.append(["3", "4", "5"])
        XCTAssertEqual(b.lines.count, 3)
        XCTAssertEqual(b.totalAppended, 5)
    }

    func testClearKeepsTotalAppendedAndCountsClears() {
        var b = LogBuffer(maxLines: 3)
        b.append(["1", "2", "3", "4"])
        XCTAssertEqual(b.clearCount, 0)
        b.clear()
        XCTAssertEqual(b.clearCount, 1)
        XCTAssertEqual(b.totalAppended, 4)
        b.append(["5"])
        XCTAssertEqual(b.totalAppended, 5)
        XCTAssertEqual(b.clearCount, 1)
    }

    func testRevisionChangesOnlyWhenContentChanges() {
        var b = LogBuffer()
        let r0 = b.revision
        b.append([])
        XCTAssertEqual(b.revision, r0)
        b.append(["x"])
        XCTAssertNotEqual(b.revision, r0)
        let r1 = b.revision
        b.clear()
        XCTAssertNotEqual(b.revision, r1)
        XCTAssertTrue(b.lines.isEmpty)
    }

    func testDefaultCapIs5000() {
        var b = LogBuffer()
        b.append((0..<6000).map(String.init))
        XCTAssertEqual(b.lines.count, 5000)
        XCTAssertEqual(b.lines.first, "1000")
    }
}
