import Foundation
import OpenAPIRuntime
import OpenAPIURLSession

typealias ItemSummary = Components.Schemas.ItemSummary
typealias ItemDetail = Components.Schemas.ItemDetail
typealias Library = Components.Schemas.Library
typealias PlaybackInfo = Components.Schemas.PlaybackInfo

/// Library, item and playback calls through the generated client (signed in).
struct MediaClient {
    let baseURL: URL
    let token: String

    private var client: Client {
        Client(serverURL: ServerURL.apiBase(baseURL), transport: URLSessionTransport(),
               middlewares: [BearerMiddleware(token: token)])
    }

    private static func fail(_ status: Int, code: String?) -> APIError {
        APIError(status: status, code: code ?? "http_\(status)")
    }

    func libraries() async throws -> [Library] {
        switch try await client.listLibraries() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func items(library id: String, offset: Int = 0, limit: Int = 100) async throws -> Components.Schemas.ItemPage {
        switch try await client.listLibraryItems(path: .init(libraryId: id), query: .init(limit: limit, offset: offset)) {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func item(_ id: String) async throws -> ItemDetail {
        switch try await client.getItem(path: .init(itemId: id)) {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func continueWatching() async throws -> [ItemSummary] {
        switch try await client.continueWatching() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func latest() async throws -> [ItemSummary] {
        switch try await client.recentlyAdded() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func favorites() async throws -> [ItemSummary] {
        switch try await client.listFavorites() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func search(_ q: String) async throws -> [ItemSummary] {
        switch try await client.search(query: .init(q: q)) {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func nextUp() async throws -> [ItemSummary] {
        switch try await client.nextUp() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    /// The episode after this one (nil after the last one).
    func nextEpisode(_ id: String) async throws -> ItemSummary? {
        switch try await client.nextEpisode(path: .init(itemId: id)) {
        case .ok(let ok): return try ok.body.json
        case .default(let status, _) where status == 404: return nil
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func preferences() async throws -> Components.Schemas.PlaybackPreferences {
        switch try await client.getPreferences() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    /// Favorite / watched for the current profile.
    func setUserData(item id: String, favorite: Bool? = nil, played: Bool? = nil) async throws {
        switch try await client.updateUserData(path: .init(itemId: id), body: .json(.init(favorite: favorite, played: played))) {
        case .noContent: return
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func profiles() async throws -> [Components.Schemas.Profile] {
        switch try await client.listProfiles() {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    /// Switches this device to a profile (PIN when it has one).
    func selectProfile(_ id: String, pin: String?) async throws {
        switch try await client.selectProfile(path: .init(profileId: id), body: .json(.init(pin: pin))) {
        case .noContent: return
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    /// Back to the account's main profile (password only needed when leaving a kids profile).
    func selectMainProfile(password: String = "") async throws {
        switch try await client.selectMainProfile(body: .json(.init(password: password))) {
        case .noContent: return
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    /// What iOS plays natively: AVPlayer handles MP4/MOV, HEVC (incl. HDR) and HLS with fMP4.
    static let device = Components.Schemas.DeviceProfile(
        containers: ["mp4", "m4v", "mov"],
        videoCodecs: ["h264", "hevc", "hdr"],
        audioCodecs: ["aac", "ac3", "eac3", "mp3", "alac", "flac"],
        hls: true)

    func startPlayback(item id: String, startMs: Int64? = nil, audioIndex: Int? = nil) async throws -> PlaybackInfo {
        let body = Components.Schemas.PlaybackRequest(audioIndex: audioIndex, startMs: startMs, device: Self.device)
        switch try await client.startPlayback(path: .init(itemId: id), body: .json(body)) {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func reportProgress(item id: String, positionMs: Int64, durationMs: Int64?) async {
        _ = try? await client.reportProgress(path: .init(itemId: id),
                                             body: .json(.init(positionMs: positionMs, durationMs: durationMs)))
    }

    /// Approves a Quick Connect code shown on another device (TV, browser).
    func authorizeQuickConnect(code: String) async throws -> String {
        switch try await client.authorizeQuickConnect(body: .json(.init(code: code))) {
        case .ok(let ok): return try ok.body.json.deviceName
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    /// Absolute URL for a server-relative link (signed stream URLs, images).
    func absolute(_ path: String) -> URL? {
        URL(string: path, relativeTo: baseURL)?.absoluteURL
    }

    func imageURL(item id: String, type: String) -> URL? {
        absolute("/api/v1/items/\(id)/images/\(type)")
    }
}
