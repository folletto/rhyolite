import Foundation

/// The persisted app settings: the vault folder, the LAN access choice and the selected tab.
public struct Settings {
    public static let vaultPathKey = "vaultPath"
    public static let allowLANKey = "allowLAN"
    public static let selectedTabKey = "selectedTab"

    private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    public var vaultPath: String {
        get { defaults.string(forKey: Self.vaultPathKey) ?? "" }
        nonmutating set { defaults.set(newValue, forKey: Self.vaultPathKey) }
    }

    /// Whether the server serves the LAN (`--access lan`) or only this machine. True when never set.
    public var allowLAN: Bool {
        get { defaults.object(forKey: Self.allowLANKey) as? Bool ?? true }
        nonmutating set { defaults.set(newValue, forKey: Self.allowLANKey) }
    }

    /// The tab the UI last showed, as a plain string so Core does not depend on the UI. Empty when never set.
    public var selectedTab: String {
        get { defaults.string(forKey: Self.selectedTabKey) ?? "" }
        nonmutating set { defaults.set(newValue, forKey: Self.selectedTabKey) }
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
