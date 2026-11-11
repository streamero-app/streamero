import Foundation

/// Where an offline download is in its life: the server prepares the file, the phone fetches it.
enum OfflineState: String, Codable, Sendable {
    /// The server job is queued.
    case waiting
    /// The server is converting the file (`transcodeProgress`).
    case transcoding
    /// The phone is fetching the file (`bytesWritten` / `bytesExpected`).
    case downloading
    /// The file is on the device and plays offline.
    case done
    case failed

    var isPending: Bool { self == .waiting || self == .transcoding || self == .downloading }
}

/// A subtitle sidecar saved next to the video (WebVTT, drawn by the player itself).
struct OfflineSubtitle: Codable, Sendable, Equatable, Hashable {
    var index: Int
    var language: String
    var forced: Bool
    var file: String
}

/// Playback state of a downloaded item, recorded on the device. `dirty` means the server
/// has not confirmed it yet.
struct OfflinePlayback: Codable, Sendable, Equatable {
    var positionMs: Int64
    var durationMs: Int64?
    var played: Bool
    var updatedAt: Date
    var dirty: Bool
}

/// One item kept offline, with everything needed to show and play it without the server.
struct OfflineEntry: Codable, Sendable, Equatable, Identifiable {
    var itemId: String
    /// Server download job; nil once the job was deleted on the server.
    var jobId: String?
    var quality: DownloadQuality
    var fileId: String?
    var state: OfflineState
    var transcodeProgress: Double = 0
    var bytesWritten: Int64 = 0
    var bytesExpected: Int64 = 0
    /// Video file name inside the server's offline folder.
    var fileName: String?
    var container: String?
    var durationMs: Int64?
    var videoBytes: Int64 = 0
    /// Subtitles and artwork.
    var extraBytes: Int64 = 0
    var subtitles: [OfflineSubtitle] = []
    var posterFile: String?
    var backdropFile: String?
    var seriesPosterFile: String?
    /// Subtitles and artwork were fetched.
    var extrasSaved = false
    var item: ItemSummary
    var overview: String?
    var series: ItemSummary?
    var addedAt: Date
    var playback: OfflinePlayback?
    var error: String?
    /// Set when the episode was fetched by a "next N episodes" subscription.
    var subscriptionSeriesId: String?

    var id: String { itemId }

    /// The series this episode belongs to (nil for movies).
    var seriesId: String? { series?.id ?? item.seriesId }

    var resumeMs: Int64 {
        guard let p = playback else { return item.positionMs ?? 0 }
        return p.played ? 0 : p.positionMs
    }

    var isPlayed: Bool { playback?.played ?? (item.played == true) }

    /// Download progress 0–1 for the current stage.
    var stageProgress: Double {
        switch state {
        case .transcoding: return min(1, max(0, transcodeProgress))
        case .downloading:
            guard bytesExpected > 0 else { return 0 }
            return min(1, Double(bytesWritten) / Double(bytesExpected))
        case .done: return 1
        default: return 0
        }
    }
}

/// "Keep the next N episodes of this series downloaded."
struct OfflineSubscription: Codable, Sendable, Equatable, Identifiable {
    var seriesId: String
    var title: String
    var count: Int
    var quality: DownloadQuality
    var addedAt: Date

    var id: String { seriesId }
}

/// Everything kept offline for one server; stored as JSON in Application Support.
struct OfflineLibrary: Codable, Sendable, Equatable {
    var entries: [OfflineEntry] = []
    var subscriptions: [OfflineSubscription] = []

    func entry(_ itemId: String) -> OfflineEntry? { entries.first { $0.itemId == itemId } }

    func index(of itemId: String) -> Int? { entries.firstIndex { $0.itemId == itemId } }

    static func decode(_ data: Data) throws -> OfflineLibrary {
        try JSONDecoder().decode(OfflineLibrary.self, from: data)
    }

    func encoded() throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(self)
    }
}

/// Folders and file names of the offline library. Safe to call from any thread (the
/// background download delegate moves finished files with it).
enum OfflinePaths {
    static let sessionIdentifier = "net.scyde.streamero.downloads"

    /// Folder name for a server id (ids are UUIDs; anything else is reduced to safe characters).
    static func safeName(_ s: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_"))
        let mapped = s.unicodeScalars.map { allowed.contains($0) ? Character($0) : "_" }
        let name = String(mapped)
        return name.isEmpty ? "server" : name
    }

    static func root() -> URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSTemporaryDirectory())
        return base.appending(path: "Offline", directoryHint: .isDirectory)
    }

    /// The server's offline folder, created on demand and kept out of iCloud backups.
    static func folder(serverId: String) -> URL {
        var url = root().appending(path: safeName(serverId), directoryHint: .isDirectory)
        if !FileManager.default.fileExists(atPath: url.path) {
            try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try? url.setResourceValues(values)
        }
        return url
    }

    static func libraryFile(serverId: String) -> URL {
        folder(serverId: serverId).appending(path: "library.json")
    }

    static func videoName(itemId: String, container: String?) -> String {
        let ext = safeName((container?.isEmpty == false ? container! : "mp4").lowercased())
        return "\(safeName(itemId)).\(ext)"
    }

    static func subtitleName(itemId: String, index: Int) -> String { "\(safeName(itemId)).sub\(index).vtt" }
    static func artworkName(itemId: String, type: String) -> String { "\(safeName(itemId)).\(type).img" }

    /// What a background download task carries so its file can be placed without the app's state.
    static func taskDescription(serverId: String, itemId: String, container: String?) -> String {
        [serverId, itemId, container ?? "mp4"].joined(separator: "|")
    }

    static func parseTaskDescription(_ s: String?) -> (serverId: String, itemId: String, container: String)? {
        guard let parts = s?.split(separator: "|", omittingEmptySubsequences: false).map(String.init),
              parts.count == 3, !parts[0].isEmpty, !parts[1].isEmpty else { return nil }
        return (parts[0], parts[1], parts[2])
    }

    static func load(serverId: String) -> OfflineLibrary {
        guard let data = try? Data(contentsOf: libraryFile(serverId: serverId)),
              let library = try? OfflineLibrary.decode(data) else { return OfflineLibrary() }
        return library
    }

    static func save(_ library: OfflineLibrary, serverId: String) {
        guard let data = try? library.encoded() else { return }
        try? data.write(to: libraryFile(serverId: serverId), options: .atomic)
    }

    static func remove(_ name: String?, serverId: String) {
        guard let name, !name.isEmpty else { return }
        try? FileManager.default.removeItem(at: folder(serverId: serverId).appending(path: name))
    }
}
