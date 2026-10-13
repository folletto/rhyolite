import XCTest
@testable import RhyoliteCore

final class SettingsTests: XCTestCase {
    private var dir: URL!
    private var defaults: UserDefaults!

    override func setUp() {
        dir = FileManager.default.temporaryDirectory.appendingPathComponent("rhyolite-test-\(UUID().uuidString)")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defaults = UserDefaults(suiteName: "rhyolite-test-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: dir)
    }

    func testEmptyIsNotConfigured() {
        XCTAssertFalse(Settings(defaults: defaults).isConfigured)
    }

    func testMissingFolderIsNotConfigured() {
        let s = Settings(defaults: defaults)
        s.vaultPath = dir.appendingPathComponent("gone").path
        XCTAssertFalse(s.isConfigured)
    }

    func testFileInsteadOfFolderIsNotConfigured() throws {
        let file = dir.appendingPathComponent("note.md")
        try "x".write(to: file, atomically: true, encoding: .utf8)
        let s = Settings(defaults: defaults)
        s.vaultPath = file.path
        XCTAssertFalse(s.isConfigured)
    }

    func testValidFolderIsConfigured() {
        let s = Settings(defaults: defaults)
        s.vaultPath = dir.path
        XCTAssertTrue(s.isConfigured)
    }

    func testPathIsPersisted() {
        Settings(defaults: defaults).vaultPath = dir.path
        XCTAssertEqual(Settings(defaults: defaults).vaultPath, dir.path)
    }

    func testFolderRemovedLaterBecomesNotConfigured() throws {
        let s = Settings(defaults: defaults)
        s.vaultPath = dir.path
        try FileManager.default.removeItem(at: dir)
        XCTAssertFalse(s.isConfigured)
    }

    func testLaunchAction() {
        XCTAssertEqual(launchAction(isConfigured: false), .showWindow)
        XCTAssertEqual(launchAction(isConfigured: true), .autoStart)
    }
}
