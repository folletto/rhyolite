// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Rhyolite",
    platforms: [.macOS(.v14)],
    targets: [
        // Everything that can be tested without a UI: settings, log buffer, child process.
        .target(name: "RhyoliteCore"),
        // The AppKit/SwiftUI shell around it.
        .executableTarget(name: "Rhyolite", dependencies: ["RhyoliteCore"]),
        .testTarget(name: "RhyoliteTests", dependencies: ["RhyoliteCore"]),
    ]
)
