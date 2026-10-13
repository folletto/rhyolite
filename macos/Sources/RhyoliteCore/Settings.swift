import Foundation

/// The persisted app settings: just the vault folder.
public struct Settings {
    public static let vaultPathKey = "vaultPath"

    private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    public var vaultPath: String {
        get { defaults.string(forKey: Self.vaultPathKey) ?? "" }
        nonmutating set { defaults.set(newValue, forKey: Self.vaultPathKey) }
    }

    /// True when the saved path exists and is a folder.
    public var isConfigured: Bool { Self.isValidFolder(vaultPath) }

    public static func isValidFolder(_ path: String) -> Bool {
        guard !path.isEmpty else { return false }
        var isDir: ObjCBool = false
        return FileManager.default.fileExists(atPath: path, isDirectory: &isDir) && isDir.boolValue
    }
}

/// What to do at launch.
public enum LaunchAction: Equatable {
    case showWindow
    case autoStart
}

public func launchAction(isConfigured: Bool) -> LaunchAction {
    isConfigured ? .autoStart : .showWindow
}
