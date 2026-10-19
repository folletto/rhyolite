import XCTest
@testable import RhyoliteCore

/// Runs ServerProcess against small shell scripts standing in for the CLI.
@MainActor
final class ServerProcessTests: XCTestCase {
    private var dir: URL!
    private var vault: URL!
    private var defaults: UserDefaults!
    private let fast = StopTimeouts(interrupt: .milliseconds(400), terminate: .milliseconds(400))

    override func setUp() async throws {
        dir = FileManager.default.temporaryDirectory.appendingPathComponent("rhyolite-test-\(UUID().uuidString)")
        vault = dir.appendingPathComponent("vault")
        try FileManager.default.createDirectory(at: vault, withIntermediateDirectories: true)
        defaults = UserDefaults(suiteName: "rhyolite-test-\(UUID().uuidString)")
    }

    override func tearDown() async throws {
        try? FileManager.default.removeItem(at: dir)
    }

    private func script(_ body: String) throws -> URL {
        let url = dir.appendingPathComponent("fake-\(UUID().uuidString).sh")
        try "#!/bin/sh\n\(body)\n".write(to: url, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        return url
    }

    private func waitFor(_ what: String, timeout: Duration = .seconds(5), _ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + timeout
        while !condition() {
            if ContinuousClock.now >= deadline { XCTFail("timed out waiting for \(what)"); return }
            try await Task.sleep(for: .milliseconds(10))
        }
    }

    /// Prints each argument on its own `arg=` line, then the working directory as `cwd=`.
    private let wellBehaved = """
    echo "▲ Rhyolite"
    for arg in "$@"; do echo "arg=$arg"; done
    echo "cwd=$(pwd -P)"
    echo "● Listening"
    echo "  on: http://127.0.0.1:1/mcp"
    trap 'echo "■ Stopping"; exit 0' INT TERM
    while true; do sleep 0.05; done
    """

    func testStartRunStop() async throws {
        let server = ServerProcess(executableURL: try script(wellBehaved), timeouts: fast, defaults: defaults)
        var states: [ServerState] = []
        server.onStateChange = { states.append($0) }

        server.start(vaultPath: vault.path)
        XCTAssertEqual(server.state, .starting)
        try await waitFor("running") { server.state == .running }
        XCTAssertGreaterThan(defaults.integer(forKey: ServerProcess.pidKey), 0)

        await server.stop()
        XCTAssertEqual(server.state, .stopped)
        XCTAssertEqual(states, [.starting, .running, .stopping, .stopped])
        XCTAssertNil(defaults.object(forKey: ServerProcess.pidKey))

        let log = server.log.lines
        XCTAssertEqual(log.first, "▲ Rhyolite")
        // The vault is the last argument and is the working directory
        // (/var is a symlink to /private/var, hence the suffix match).
        let vaultSuffix = "\(dir.lastPathComponent)/vault"
        XCTAssertEqual(argumentsSeen(server).last, vault.path)
        XCTAssertTrue(log.contains { $0.hasPrefix("cwd=") && $0.hasSuffix(vaultSuffix) }, "\(log)")
        XCTAssertTrue(log.contains("■ Stopping"), "the CLI's shutdown output is kept")
    }

    /// The arguments the fake CLI was started with, as `wellBehaved` echoed them.
    private func argumentsSeen(_ server: ServerProcess) -> [String] {
        server.log.lines.filter { $0.hasPrefix("arg=") }.map { String($0.dropFirst("arg=".count)) }
    }

    func testStartsWithLANAccessByDefault() async throws {
        let server = ServerProcess(executableURL: try script(wellBehaved), timeouts: fast)
        server.start(vaultPath: vault.path)
        try await waitFor("running") { server.state == .running }
        XCTAssertEqual(argumentsSeen(server), ["--access", "lan", vault.path])
        await server.stop()
    }

    func testLocalAccessIsPassedBeforeTheVault() async throws {
        let server = ServerProcess(executableURL: try script(wellBehaved), timeouts: fast)
        server.start(vaultPath: vault.path, access: .local)
        try await waitFor("running") { server.state == .running }
        XCTAssertEqual(argumentsSeen(server), ["--access", "local", vault.path])
        await server.stop()
    }

    func testOutputFromBothStreamsIsShown() async throws {
        let exe = try script("echo out; echo err >&2; echo \"● Listening\"; sleep 30")
        let server = ServerProcess(executableURL: exe, timeouts: fast)
        server.start(vaultPath: vault.path)
        try await waitFor("running") { server.state == .running }
        XCTAssertTrue(server.log.lines.contains("out"))
        XCTAssertTrue(server.log.lines.contains("err"))
        await server.stop()
    }

    func testEarlyExitIsAFailureWithItsOutput() async throws {
        let exe = try script("echo 'listen tcp: address already in use' >&2; exit 1")
        let server = ServerProcess(executableURL: exe, timeouts: fast, defaults: defaults)
        server.start(vaultPath: vault.path)
        try await waitFor("failed") { if case .failed = server.state { true } else { false } }
        XCTAssertEqual(server.state, .failed("exit code 1"))
        XCTAssertTrue(server.log.lines.contains("listen tcp: address already in use"))
        XCTAssertNil(defaults.object(forKey: ServerProcess.pidKey))
        XCTAssertFalse(server.isActive)
    }

    func testCanRestartAfterFailure() async throws {
        let exe = try script("exit 3")
        let server = ServerProcess(executableURL: exe, timeouts: fast)
        server.start(vaultPath: vault.path)
        try await waitFor("failed") { server.state == .failed("exit code 3") }
        server.start(vaultPath: vault.path)
        XCTAssertEqual(server.state, .starting)
        try await waitFor("failed again") { server.state == .failed("exit code 3") }
    }

    func testMissingExecutableFails() {
        let server = ServerProcess(executableURL: dir.appendingPathComponent("nope"), timeouts: fast)
        server.start(vaultPath: vault.path)
        XCTAssertEqual(server.state, .failed("cannot launch"))
        XCTAssertTrue(server.log.lines.first?.hasPrefix("Cannot start rhyolite") == true)
    }

    func testStartWhileActiveIsIgnored() async throws {
        let server = ServerProcess(executableURL: try script(wellBehaved), timeouts: fast)
        server.start(vaultPath: vault.path)
        try await waitFor("running") { server.state == .running }
        let before = server.log.revision
        server.start(vaultPath: vault.path)
        XCTAssertEqual(server.state, .running)
        XCTAssertEqual(server.log.revision, before, "a second start must not clear the log")
        await server.stop()
    }

    func testEscalatesToTerminateWhenInterruptIsIgnored() async throws {
        let exe = try script("""
        trap '' INT
        trap 'exit 0' TERM
        echo "● Listening"
        while true; do sleep 0.05; done
        """)
        let server = ServerProcess(executableURL: exe, timeouts: fast)
        server.start(vaultPath: vault.path)
        try await waitFor("running") { server.state == .running }
        await server.stop()
        XCTAssertEqual(server.state, .stopped)
        XCTAssertTrue(server.log.lines.contains("rhyolite did not stop on interrupt; terminating"))
        XCTAssertFalse(server.log.lines.contains("rhyolite did not terminate; killing"))
    }

    func testEscalatesToKillWhenNothingElseWorks() async throws {
        let exe = try script("""
        trap '' INT TERM
        echo "● Listening"
        while true; do sleep 0.05; done
        """)
        let server = ServerProcess(executableURL: exe, timeouts: fast)
        server.start(vaultPath: vault.path)
        try await waitFor("running") { server.state == .running }
        await server.stop()
        XCTAssertEqual(server.state, .stopped)
        XCTAssertTrue(server.log.lines.contains("rhyolite did not terminate; killing"))
    }

    func testStopWhileStartingStopsCleanly() async throws {
        // No ready marker is ever printed.
        let exe = try script("trap 'exit 0' INT TERM; while true; do sleep 0.05; done")
        let server = ServerProcess(executableURL: exe, timeouts: fast)
        server.start(vaultPath: vault.path)
        XCTAssertEqual(server.state, .starting)
        await server.stop()
        XCTAssertEqual(server.state, .stopped)
    }

    func testStopWhenNotRunningIsANoOp() async {
        let server = ServerProcess(executableURL: dir.appendingPathComponent("unused"), timeouts: fast)
        await server.stop()
        XCTAssertEqual(server.state, .stopped)
    }

    func testStopIsPromptWhenTheCLIExitsCleanly() async throws {
        let server = ServerProcess(executableURL: try script(wellBehaved), timeouts: StopTimeouts(interrupt: .seconds(8), terminate: .seconds(2)))
        server.start(vaultPath: vault.path)
        try await waitFor("running") { server.state == .running }
        let t0 = ContinuousClock.now
        await server.stop()
        XCTAssertLessThan(ContinuousClock.now - t0, .seconds(3), "pipes must reach EOF; no waiting out the timeouts")
    }
}

@MainActor
final class OrphanReaperTests: XCTestCase {
    private var defaults: UserDefaults!
    private let sleepURL = URL(fileURLWithPath: "/bin/sleep")

    override func setUp() async throws {
        defaults = UserDefaults(suiteName: "rhyolite-test-\(UUID().uuidString)")
    }

    private func spawnSleep() throws -> Process {
        let p = Process()
        p.executableURL = sleepURL
        p.arguments = ["60"]
        try p.run()
        return p
    }

    func testStopsAnOrphanThatIsOurExecutable() async throws {
        let p = try spawnSleep()
        defer { if p.isRunning { p.terminate() } }
        defaults.set(Int(p.processIdentifier), forKey: ServerProcess.pidKey)

        let reaped = await OrphanReaper.reap(defaults: defaults, executableURL: sleepURL, timeout: .seconds(2))
        XCTAssertTrue(reaped)
        p.waitUntilExit()
        XCTAssertEqual(p.terminationReason, .uncaughtSignal)
        XCTAssertNil(defaults.object(forKey: ServerProcess.pidKey))
    }

    func testLeavesAReusedPIDAlone() async throws {
        let p = try spawnSleep()
        defer { p.terminate() }
        defaults.set(Int(p.processIdentifier), forKey: ServerProcess.pidKey)

        let other = URL(fileURLWithPath: "/usr/bin/true")
        let reaped = await OrphanReaper.reap(defaults: defaults, executableURL: other, timeout: .seconds(1))
        XCTAssertFalse(reaped)
        XCTAssertTrue(p.isRunning, "a process that isn't our CLI must not be signalled")
        XCTAssertNil(defaults.object(forKey: ServerProcess.pidKey))
    }

    func testNothingRecordedOrAlreadyGone() async throws {
        let none = await OrphanReaper.reap(defaults: defaults, executableURL: sleepURL)
        XCTAssertFalse(none)

        let p = try spawnSleep()
        p.terminate()
        p.waitUntilExit()
        defaults.set(Int(p.processIdentifier), forKey: ServerProcess.pidKey)
        let gone = await OrphanReaper.reap(defaults: defaults, executableURL: sleepURL)
        XCTAssertFalse(gone)
    }
}
