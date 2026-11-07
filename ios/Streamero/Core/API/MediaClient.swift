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

    /// What iOS plays natively: AVPlayer handles MP4/MOV, HEVC (incl. HDR) and HLS with fMP4.
    static let device = Components.Schemas.DeviceProfile(
        containers: ["mp4", "m4v", "mov"],
        videoCodecs: ["h264", "hevc", "hdr"],
        audioCodecs: ["aac", "ac3", "eac3", "mp3", "alac", "flac"],
        hls: true)

    func startPlayback(item id: String, startMs: Int64? = nil) async throws -> PlaybackInfo {
        let body = Components.Schemas.PlaybackRequest(startMs: startMs, device: Self.device)
        switch try await client.startPlayback(path: .init(itemId: id), body: .json(body)) {
        case .ok(let ok): return try ok.body.json
        case .default(let status, let r): throw Self.fail(status, code: try? r.body.json.code)
        }
    }

    func reportProgress(item id: String, positionMs: Int64, durationMs: Int64?) async {
        _ = try? await client.reportProgress(path: .init(itemId: id),
                                             body: .json(.init(positionMs: positionMs, durationMs: durationMs)))
    }

    /// Absolute URL for a server-relative link (signed stream URLs, images).
    func absolute(_ path: String) -> URL? {
        URL(string: path, relativeTo: baseURL)?.absoluteURL
    }

    func imageURL(item id: String, type: String) -> URL? {
        absolute("/api/v1/items/\(id)/images/\(type)")
    }
}
