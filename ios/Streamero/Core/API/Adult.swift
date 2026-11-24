import Foundation
import Observation
import UIKit

// Adult section (D-053): models mirroring api/openapi.yaml, the client calls and the pure
// helpers the screens use (tested in AdultTests). Every /adult endpoint answers 404 to an
// account without access, and content endpoints answer 403 `adult_locked` until the
// viewer's PIN was entered in this session.

struct AdultLibrary: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var sceneCount: Int
}

struct AdultStatus: Codable, Sendable, Equatable {
    var pinSet: Bool
    var unlocked: Bool
    var blur: Bool
    var metadataConfigured: Bool
    var libraries: [AdultLibrary]
}

struct AdultPreferencesInput: Codable, Sendable, Equatable {
    var blur: Bool?
    /// New PIN (4–12 digits); an empty string removes it.
    var pin: String?
    /// Needed to change or remove an existing PIN.
    var currentPin: String?
}

struct AdultRef: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var name: String
}

struct AdultScene: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var libraryId: String
    /// "scene" or "movie".
    var kind: String
    var title: String
    var date: String?
    var year: Int?
    var studio: AdultRef?
    var performers: [AdultRef]
    var tags: [String]
    var durationMs: Int64?
    var height: Int?
    var hasPoster: Bool
    var hasBackdrop: Bool
    var positionMs: Int64
    var played: Bool
    var addedAt: String
}

struct AdultFile: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var name: String
    var size: Int64
    var container: String
    var videoCodec: String?
    var width: Int?
    var height: Int?
    var durationMs: Int64?
}

/// A scene with its files and description (the spec's allOf, flattened).
struct AdultSceneDetail: Codable, Sendable, Equatable {
    var id: String
    var libraryId: String
    var kind: String
    var title: String
    var date: String?
    var year: Int?
    var studio: AdultRef?
    var performers: [AdultRef]
    var tags: [String]
    var durationMs: Int64?
    var height: Int?
    var hasPoster: Bool
    var hasBackdrop: Bool
    var positionMs: Int64
    var played: Bool
    var addedAt: String
    var description: String?
    var url: String?
    var files: [AdultFile]
    var providerIds: [String: String]
    var metadataError: String?
}

struct AdultScenePage: Codable, Sendable, Equatable {
    var items: [AdultScene]
    var total: Int
}

struct AdultStudio: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var name: String
    var sceneCount: Int
    var hasImage: Bool
    var url: String?
    var parent: String?
}

struct AdultStudioPage: Codable, Sendable, Equatable {
    var items: [AdultStudio]
    var total: Int
}

struct AdultPerformer: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var name: String
    var sceneCount: Int
    var hasImage: Bool
    var aliases: [String]
    var gender: String?
    var birthdate: String?
    var country: String?
    var description: String?
}

struct AdultPerformerPage: Codable, Sendable, Equatable {
    var items: [AdultPerformer]
    var total: Int
}

/// Filters of the scene list; `query` is what goes on the wire.
struct AdultSceneFilter: Equatable, Hashable, Sendable {
    var libraryId: String? = nil
    var studioId: String? = nil
    var performerId: String? = nil
    /// "" = both, "scene" or "movie".
    var kind = ""
    var q = ""
    var tag: String? = nil
    var year: Int? = nil
    /// all, unwatched, inProgress or watched.
    var status = "all"
    /// date, added, title or played.
    var sort = "date"

    func query(offset: Int, limit: Int = 60) -> [String: String?] {
        let text = q.trimmingCharacters(in: .whitespacesAndNewlines)
        return [
            "libraryId": libraryId,
            "studioId": studioId,
            "performerId": performerId,
            "kind": kind.isEmpty ? nil : kind,
            "q": text.isEmpty ? nil : text,
            "tag": tag,
            "year": year.map(String.init),
            "status": status == "all" ? nil : status,
            "sort": sort,
            "offset": String(offset),
            "limit": String(limit),
        ]
    }
}

extension MediaClient {
    func adultStatus() async throws -> AdultStatus { try await get("adult/status") }

    func unlockAdult(pin: String) async throws {
        try await send("POST", "adult/unlock", ["pin": pin])
    }

    func lockAdult() async throws { try await send("POST", "adult/lock") }

    func updateAdultPreferences(_ input: AdultPreferencesInput) async throws -> AdultStatus {
        try await put("adult/preferences", input)
    }

    func adultScenes(_ filter: AdultSceneFilter, offset: Int, limit: Int = 60) async throws -> AdultScenePage {
        try await get("adult/scenes", query: filter.query(offset: offset, limit: limit))
    }

    func adultScene(_ id: String) async throws -> AdultSceneDetail { try await get("adult/scenes/\(id)") }

    func adultStudios(q: String, offset: Int, limit: Int = 100) async throws -> AdultStudioPage {
        try await get("adult/studios", query: AdultGate.nameQuery(q: q, offset: offset, limit: limit))
    }

    func adultStudio(_ id: String) async throws -> AdultStudio { try await get("adult/studios/\(id)") }

    func adultPerformers(q: String, offset: Int, limit: Int = 100) async throws -> AdultPerformerPage {
        try await get("adult/performers", query: AdultGate.nameQuery(q: q, offset: offset, limit: limit))
    }

    func adultPerformer(_ id: String) async throws -> AdultPerformer { try await get("adult/performers/\(id)") }

    /// Same decision and signed URLs as `startPlayback`; no subtitles or segments.
    func startAdultPlayback(scene id: String, startMs: Int64? = nil, audioIndex: Int? = nil, fileId: String? = nil,
                            profile: DeviceProfile = MediaClient.device) async throws -> PlaybackInfo {
        try await post("adult/scenes/\(id)/playback", PlaybackRequest(device: profile, fileId: fileId, audioIndex: audioIndex,
                                                                     startMs: startMs))
    }

    func reportAdultProgress(scene id: String, positionMs: Int64, durationMs: Int64?) async {
        try? await send("PUT", "adult/scenes/\(id)/progress", ProgressReport(positionMs: positionMs, durationMs: durationMs))
    }

    /// `owner` is scene, studio or performer; scenes have poster and backdrop, the others image.
    func adultImageURL(owner: String, id: String, type: String) -> URL? {
        absolute("/api/v1/adult/images/\(owner)/\(id)/\(type)")
    }
}

/// The rules of the section, pure so they can be tested.
enum AdultGate {
    /// The entry point shows only when the server says so for this session (granted account,
    /// an adult library exists, not a kids profile).
    static func showsEntry(_ user: User?) -> Bool {
        guard let user else { return false }
        return user.adultAccess == true && user.currentProfile?.isKid != true
    }

    /// Content endpoints refuse with this until the PIN is entered.
    static func isLocked(_ error: Error) -> Bool {
        guard let api = error as? APIError else { return false }
        return api.status == 403 && api.code == "adult_locked"
    }

    /// The section disappeared for this account (access removed, library deleted).
    static func isGone(_ error: Error) -> Bool {
        (error as? APIError)?.status == 404
    }

    /// Whether the status asks for the PIN before any content loads.
    static func needsPin(_ status: AdultStatus) -> Bool { status.pinSet && !status.unlocked }

    /// A PIN the server accepts: 4 to 12 digits.
    static func validPin(_ pin: String) -> Bool {
        (4...12).contains(pin.count) && pin.allSatisfy { $0.isASCII && $0.isNumber }
    }

    /// Artwork stays blurred while blurring is on and the picture was not tapped.
    static func blurred(blur: Bool, revealed: Set<String>, key: String) -> Bool {
        blur && !revealed.contains(key)
    }

    static func nameQuery(q: String, offset: Int, limit: Int) -> [String: String?] {
        let text = q.trimmingCharacters(in: .whitespacesAndNewlines)
        return ["q": text.isEmpty ? nil : text, "offset": String(offset), "limit": String(limit)]
    }

    /// 0…1 of a scene watched so far (nil when not started or finished).
    static func progress(positionMs: Int64, durationMs: Int64?, played: Bool) -> Double? {
        guard !played, positionMs > 0, let d = durationMs, d > 0 else { return nil }
        return min(1, Double(positionMs) / Double(d))
    }
}

extension Library {
    /// Adult libraries are listed to administrators for managing only; their content is
    /// reached through the adult section, never browsed as a library.
    var isAdult: Bool { kind == "adult" }
}

/// The adult section's state for this sign-in: status, whether the PIN is needed, which
/// pictures were revealed. Locks again when the app goes to the background.
@MainActor
@Observable
final class AdultSession {
    private(set) var status: AdultStatus?
    /// The server asked for the PIN (status or a content endpoint said so).
    private(set) var locked = false
    /// Bumped after unlocking, so screens reload what failed.
    private(set) var unlockTick = 0
    /// Image keys ("scene/<id>") the viewer tapped to see unblurred.
    var revealed: Set<String> = []

    var blur: Bool { status?.blur ?? true }

    func refresh(client: MediaClient) async throws {
        let s = try await client.adultStatus()
        status = s
        locked = AdultGate.needsPin(s)
    }

    /// Notes a content error; true when it means the PIN is needed.
    @discardableResult
    func note(_ error: Error) -> Bool {
        guard AdultGate.isLocked(error) else { return false }
        locked = true
        return true
    }

    func unlock(client: MediaClient, pin: String) async throws {
        try await client.unlockAdult(pin: pin)
        locked = false
        if var s = status {
            s.unlocked = true
            status = s
        }
        unlockTick += 1
    }

    /// Locks the section on the server (when a PIN is set) and forgets what was shown.
    func lock(client: MediaClient) async {
        forget()
        guard status?.pinSet == true else { return }
        try? await client.lockAdult()
        locked = true
        if var s = status {
            s.unlocked = false
            status = s
        }
    }

    func apply(_ status: AdultStatus) {
        self.status = status
        locked = AdultGate.needsPin(status)
    }

    /// Drops revealed pictures and the in-memory artwork.
    func forget() {
        revealed = []
        AdultImageCache.shared.clear()
    }

    /// After sign-out or a profile switch.
    func reset() {
        forget()
        status = nil
        locked = false
    }

    func isBlurred(_ key: String) -> Bool { AdultGate.blurred(blur: blur, revealed: revealed, key: key) }

    func reveal(_ key: String) { revealed.insert(key) }
}

/// Adult artwork: memory only. The session has no URL cache and the decoded pictures live in
/// an NSCache that is emptied when the section locks or the app goes to the background.
@MainActor
final class AdultImageCache {
    static let shared = AdultImageCache()
    private let cache = NSCache<NSURL, UIImage>()
    private let session: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.urlCache = nil
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        return URLSession(configuration: config)
    }()

    func image(url: URL, token: String) async -> UIImage? {
        if let hit = cache.object(forKey: url as NSURL) { return hit }
        var req = URLRequest(url: url)
        req.cachePolicy = .reloadIgnoringLocalCacheData
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        guard let (data, resp) = try? await session.data(for: req),
              (resp as? HTTPURLResponse)?.statusCode == 200,
              let img = UIImage(data: data) else { return nil }
        cache.setObject(img, forKey: url as NSURL)
        return img
    }

    func clear() { cache.removeAllObjects() }
}
