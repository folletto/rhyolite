import Darwin
import Foundation
import Observation

public enum ServerState: Equatable, Sendable {
    case stopped
    case starting
    case running
    case stopping
    case failed(String)
}

/// Which network the CLI serves: the LAN, or only this machine. Maps to its `--access` flag.
public enum AccessMode: String, Sendable {
    case lan
    case local
}

/// How long to wait at each step when stopping before escalating.
public struct StopTimeouts: Sendable {
    /// After SIGINT. The CLI's own graceful shutdown takes up to 5 s.
    public var interrupt: Duration
    /// After SIGTERM, before SIGKILL.
    public var terminate: Duration

    public init(interrupt: Duration, terminate: Duration) {
        self.interrupt = interrupt
        self.terminate = terminate
    }

    public static let standard = StopTimeouts(interrupt: .seconds(8), terminate: .seconds(2))
}

/// Owns the embedded `rhyolite` CLI as a child process: starts it on a vault
/// folder, streams its output, and stops it gracefully.
@MainActor
@Observable
public final class ServerProcess {
    /// UserDefaults key holding the child's PID while it runs (see `OrphanReaper`).
    nonisolated public static let pidKey = "serverPID"
    /// The CLI prints this once it has bound its port.
    nonisolated static let readyMarker = "● Listening"

    public private(set) var state: ServerState = .stopped
    public private(set) var log = LogBuffer()

    /// Called after every state change.
    @ObservationIgnored public var onStateChange: ((ServerState) -> Void)?
    /// Called with each batch of output lines, after they are added to the log.
    @ObservationIgnored public var onLines: (([String]) -> Void)?

    @ObservationIgnored private let executableURL: URL
    @ObservationIgnored private let timeouts: StopTimeouts
    @ObservationIgnored private let defaults: UserDefaults?
    @ObservationIgnored private var process: Process?
    @ObservationIgnored private var runID = 0

    public init(executableURL: URL, timeouts: StopTimeouts = .standard, defaults: UserDefaults? = nil) {
        self.executableURL = executableURL
        self.timeouts = timeouts
        self.defaults = defaults
    }

    /// True from the moment a start is requested until the process is gone.
    public var isActive: Bool {
        switch state {
        case .starting, .running, .stopping: true
        case .stopped, .failed: false
        }
    }

    /// Starts the CLI on `vaultPath`, serving either the LAN or only this machine, per `access`.
    public func start(vaultPath: String, access: AccessMode = .lan) {
        guard !isActive else { return }
        log.clear()
        setState(.starting)

        let p = Process()
        p.executableURL = executableURL
        // Flags must come before the folder: the CLI's flag parsing stops at the first positional argument.
        p.arguments = ["--access", access.rawValue, vaultPath]
        p.currentDirectoryURL = URL(fileURLWithPath: vaultPath, isDirectory: true)
        p.standardInput = FileHandle.nullDevice
        let out = Pipe()
        let err = Pipe()
        p.standardOutput = out
        p.standardError = err

        runID += 1
        let id = runID
        let readers = DispatchGroup()
        p.terminationHandler = { [weak self] proc in
            let status = proc.terminationStatus
            let signaled = proc.terminationReason == .uncaughtSignal
            // Let the readers drain both pipes before announcing the exit, so no
            // output is lost and it always arrives before the state change.
            DispatchQueue.global().async {
                _ = readers.wait(timeout: .now() + 2)
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { self?.processExited(id: id, status: status, signaled: signaled) }
                }
            }
        }

        do {
            try p.run()
        } catch {
            log.append(["Cannot start rhyolite: \(error.localizedDescription)"])
            setState(.failed("cannot launch"))
            return
        }
        process = p
        defaults?.set(Int(p.processIdentifier), forKey: Self.pidKey)

        for handle in [out.fileHandleForReading, err.fileHandleForReading] {
            read(handle, group: readers)
        }
    }

    /// Stops the CLI and returns once it has exited: SIGINT (like Ctrl+C),
    /// then SIGTERM, then SIGKILL, waiting for each in turn.
    public func stop() async {
        switch state {
        case .stopped, .failed:
            return
        case .stopping:
            _ = await waitUntilExited(timeout: timeouts.interrupt + timeouts.terminate + .seconds(2))
            return
        case .starting, .running:
            break
        }
        setState(.stopping)
        guard let p = process else {
            setState(.stopped)
            return
        }
        if p.isRunning { p.interrupt() }
        if await waitUntilExited(timeout: timeouts.interrupt) { return }

        appendLog("rhyolite did not stop on interrupt; terminating")
        if p.isRunning { p.terminate() }
        if await waitUntilExited(timeout: timeouts.terminate) { return }

        appendLog("rhyolite did not terminate; killing")
        if p.isRunning { kill(p.processIdentifier, SIGKILL) }
        _ = await waitUntilExited(timeout: .seconds(2))
    }

    // MARK: - Internals

    private func setState(_ new: ServerState) {
        guard new != state else { return }
        state = new
        onStateChange?(new)
    }

    private func appendLog(_ line: String) {
        log.append([line])
    }

    private func consume(_ lines: [String]) {
        log.append(lines)
        if state == .starting, lines.contains(where: { $0.contains(Self.readyMarker) }) {
            setState(.running)
        }
        onLines?(lines)
    }

    private func processExited(id: Int, status: Int32, signaled: Bool) {
        guard id == runID else { return }
        process = nil
        defaults?.removeObject(forKey: Self.pidKey)
        let how = signaled ? "killed by signal \(status)" : "exit code \(status)"
        switch state {
        case .stopping:
            appendLog("— rhyolite stopped")
            setState(.stopped)
        case .stopped, .failed:
            break
        case .starting, .running:
            appendLog("— rhyolite exited unexpectedly (\(how))")
            setState(.failed(how))
        }
    }

    /// Polls rather than juggling continuations: stops are rare and short.
    private func waitUntilExited(timeout: Duration) async -> Bool {
        let deadline = ContinuousClock.now + timeout
        while process != nil {
            if ContinuousClock.now >= deadline { return false }
            try? await Task.sleep(for: .milliseconds(20))
        }
        return true
    }

    /// Reads one pipe on a dedicated thread until EOF, handing complete lines to the main queue in order.
    private func read(_ handle: FileHandle, group: DispatchGroup) {
        group.enter()
        let thread = Thread { [weak self] in
            var splitter = LineSplitter()
            let deliver: @Sendable ([String]) -> Void = { lines in
                guard !lines.isEmpty else { return }
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { self?.consume(lines) }
                }
            }
            // POSIX read returns as soon as any data is available;
            // FileHandle.read(upToCount:) would wait to fill its buffer.
            let fd = handle.fileDescriptor
            var buffer = [UInt8](repeating: 0, count: 16 * 1024)
            while true {
                let n = buffer.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress, $0.count) }
                if n < 0 && errno == EINTR { continue }
                if n <= 0 { break }
                deliver(splitter.feed(Data(buffer[..<n])))
            }
            if let tail = splitter.flush() { deliver([tail]) }
            group.leave()
        }
        thread.start()
    }
}

/// Cleans up a CLI left running by a previous crash or force-quit of the app.
@MainActor
public enum OrphanReaper {
    /// Stops the process recorded under `ServerProcess.pidKey` if it is still
    /// alive and really is our CLI (the PID may have been reused by something
    /// else). Returns true if it stopped one.
    @discardableResult
    public static func reap(defaults: UserDefaults, executableURL: URL, timeout: Duration = .seconds(8)) async -> Bool {
        let stored = defaults.integer(forKey: ServerProcess.pidKey)
        defaults.removeObject(forKey: ServerProcess.pidKey)
        guard stored > 0 else { return false }
        let pid = pid_t(stored)
        guard kill(pid, 0) == 0, path(of: pid) == executableURL.resolvingSymlinksInPath().path else {
            return false
        }
        kill(pid, SIGINT)
        let deadline = ContinuousClock.now + timeout
        while kill(pid, 0) == 0 {
            if ContinuousClock.now >= deadline {
                kill(pid, SIGKILL)
                break
            }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return true
    }

    private static func path(of pid: pid_t) -> String? {
        var buffer = [CChar](repeating: 0, count: 4096)
        let length = Int(proc_pidpath(pid, &buffer, UInt32(buffer.count)))
        guard length > 0 else { return nil }
        let path = String(decoding: buffer[..<length].map { UInt8(bitPattern: $0) }, as: UTF8.self)
        return URL(fileURLWithPath: path).resolvingSymlinksInPath().path
    }
}
