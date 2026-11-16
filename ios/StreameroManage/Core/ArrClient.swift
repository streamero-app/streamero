import Foundation

/// The six services Streamero Manage controls through the server's *arr proxy (D-048).
enum ArrService: String, CaseIterable, Identifiable, Sendable, Codable, Hashable {
    case radarr, sonarr, lidarr, prowlarr, bazarr, seerr

    var id: String { rawValue }

    /// Product names stay as they are in every language.
    var name: String {
        switch self {
        case .radarr: "Radarr"
        case .sonarr: "Sonarr"
        case .lidarr: "Lidarr"
        case .prowlarr: "Prowlarr"
        case .bazarr: "Bazarr"
        case .seerr: "Seerr"
        }
    }

    /// The API prefix when the server does not say (it normally does, `apiBase`).
    var defaultApiBase: String {
        switch self {
        case .radarr, .sonarr: "api/v3"
        case .lidarr, .prowlarr, .seerr: "api/v1"
        case .bazarr: "api"
        }
    }

    var symbol: String {
        switch self {
        case .radarr: "film.stack"
        case .sonarr: "tv"
        case .lidarr: "music.note.list"
        case .prowlarr: "antenna.radiowaves.left.and.right"
        case .bazarr: "captions.bubble"
        case .seerr: "tray.and.arrow.down"
        }
    }

    /// Radarr, Sonarr and Lidarr: a library of titles with files, queue and wanted lists.
    var hasLibrary: Bool { self == .radarr || self == .sonarr || self == .lidarr }
    /// Shares the Servarr API shapes (system, health, tasks, backups, providers with schemas).
    var isServarr: Bool { hasLibrary || self == .prowlarr }
}

/// One entry of `GET /api/v1/arr/services`.
struct ArrServiceStatus: Decodable, Sendable, Hashable, Identifiable {
    var id: String
    var configured: Bool
    var reachable: Bool
    var version: String?
    var error: String?
    var apiBase: String?

    var service: ArrService? { ArrService(rawValue: id) }
}

struct ArrServicesResponse: Decodable, Sendable {
    var services: [ArrServiceStatus]
}

/// A field-level problem from a Servarr validation response (`[{propertyName, errorMessage, severity}]`).
struct ArrValidationFailure: Hashable, Sendable {
    var propertyName: String
    var message: String
    var isWarning: Bool

    /// Whether this failure is about a form field ("BaseUrl" matches the field "baseUrl").
    func matches(_ name: String) -> Bool {
        let a = propertyName.lowercased(), b = name.lowercased()
        return !a.isEmpty && (a == b || a.hasSuffix("." + b))
    }
}

/// What went wrong talking to a service through the proxy.
enum ArrError: Error, Equatable, Sendable {
    case unauthenticated
    case forbidden
    /// The service has no address or API key on the server.
    case notConfigured
    /// The server could not reach the service.
    case unreachable(String?)
    case invalidPath
    case tooLarge
    /// The service rejected the input; field errors are shown next to the fields.
    case validation([ArrValidationFailure])
    /// Any other status, with the service's own (English) message when it gave one.
    case http(Int, String?)
    case network

    /// Maps an error response. The proxy's own errors are `{code, message}`; everything else
    /// is the service's answer passed through.
    static func from(status: Int, data: Data) -> ArrError {
        let json = try? JSONValue.parse(data)
        if let code = json?["code"].string {
            switch code {
            case "unauthenticated": return .unauthenticated
            case "forbidden": return .forbidden
            case "arr_not_configured": return .notConfigured
            case "arr_unreachable": return .unreachable(json?["message"].string)
            case "invalid_path": return .invalidPath
            case "request_too_large": return .tooLarge
            default: break
            }
        }
        if status == 413 { return .tooLarge }
        if let list = json?.array, !list.isEmpty {
            let failures = list.compactMap { item -> ArrValidationFailure? in
                guard let message = item["errorMessage"].string ?? item["message"].string else { return nil }
                let severity = item["severity"].string?.lowercased() ?? item["severity"].text.lowercased()
                return ArrValidationFailure(propertyName: item["propertyName"].string ?? "", message: message,
                                            isWarning: severity == "warning" || item["isWarning"].bool == true)
            }
            if !failures.isEmpty { return .validation(failures) }
        }
        let message = json?["message"].string ?? json?["error"].string ?? json?["errorMessage"].string
            ?? (json == nil ? String(decoding: data.prefix(300), as: UTF8.self).nilIfBlank : nil)
        if status == 400, let message { return .validation([ArrValidationFailure(propertyName: "", message: message, isWarning: false)]) }
        return .http(status, message)
    }

    var failures: [ArrValidationFailure] {
        if case .validation(let f) = self { return f }
        return []
    }
}

extension String {
    var nilIfBlank: String? {
        let t = trimmingCharacters(in: .whitespacesAndNewlines)
        return t.isEmpty ? nil : t
    }
}

/// Talks to one service through `/api/v1/arr/{service}/proxy/{apiBase}/…` with the session
/// token; the server adds the service's API key. Bodies are the services' own JSON.
struct ArrClient: Sendable {
    let baseURL: URL
    let token: String
    let service: ArrService
    let apiBase: String

    init(baseURL: URL, token: String, service: ArrService, apiBase: String? = nil) {
        self.baseURL = baseURL
        self.token = token
        self.service = service
        let base = (apiBase ?? service.defaultApiBase).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        self.apiBase = base.isEmpty ? service.defaultApiBase : base
    }

    init(media: MediaClient, service: ArrService, apiBase: String? = nil) {
        self.init(baseURL: media.baseURL, token: media.token, service: service, apiBase: apiBase)
    }

    /// `…/api/v1/arr/{service}/proxy`.
    var proxyRoot: URL {
        ServerURL.apiBase(baseURL).appending(path: "arr").appending(path: service.rawValue).appending(path: "proxy")
    }

    /// The URL of an API resource: "movie/12" → …/proxy/api/v3/movie/12?…
    func url(_ resource: String, query: [URLQueryItem] = []) -> URL {
        var url = proxyRoot
        for part in apiBase.split(separator: "/") { url = url.appending(path: String(part)) }
        for part in resource.split(separator: "/") { url = url.appending(path: String(part)) }
        return Self.withQuery(url, query)
    }

    /// Adds query items, encoding everything but unreserved characters ("&", "+", "=" in a
    /// search term must not end the value).
    static func withQuery(_ url: URL, _ query: [URLQueryItem]) -> URL {
        guard !query.isEmpty, var comps = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return url }
        let allowed = Self.unreserved
        let encoded = query.map { item in
            (item.name.addingPercentEncoding(withAllowedCharacters: allowed) ?? item.name) + "=" +
                ((item.value ?? "").addingPercentEncoding(withAllowedCharacters: allowed) ?? "")
        }.joined(separator: "&")
        comps.percentEncodedQuery = encoded
        return comps.url ?? url
    }

    /// A path outside the API prefix ("MediaCover/1/poster.jpg?lastWrite=1", "backup/manual/x.zip").
    func rawURL(_ path: String) -> URL {
        let parts = path.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)
        var url = proxyRoot
        for part in (parts.first ?? "").split(separator: "/") { url = url.appending(path: String(part)) }
        if parts.count > 1, let items = URLComponents(string: "?" + parts[1])?.queryItems, !items.isEmpty {
            url = Self.withQuery(url, items)
        }
        return url
    }

    /// Artwork the service caches (`images[].url`, e.g. "/radarr/MediaCover/1/poster.jpg?lastWrite=…").
    func mediaCoverURL(_ imageURL: String?) -> URL? {
        guard let imageURL, let range = imageURL.range(of: "MediaCover/") else { return nil }
        return rawURL(String(imageURL[range.lowerBound...]))
    }

    // MARK: Requests

    func data(_ method: String, url: URL, body: Data? = nil, contentType: String? = nil, timeout: TimeInterval = 60) async throws -> Data {
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.timeoutInterval = timeout
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if let contentType { req.setValue(contentType, forHTTPHeaderField: "Content-Type") }
        req.httpBody = body
        let data: Data, response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: req)
        } catch {
            throw ArrError.network
        }
        guard let http = response as? HTTPURLResponse else { throw ArrError.network }
        guard (200..<300).contains(http.statusCode) else {
            let error = ArrError.from(status: http.statusCode, data: data)
            if error == .unauthenticated { SessionExpiry.notify(token: token) }
            throw error
        }
        return data
    }

    func get(_ resource: String, query: [URLQueryItem] = [], timeout: TimeInterval = 60) async throws -> JSONValue {
        try JSONValue.parse(try await data("GET", url: url(resource, query: query), timeout: timeout))
    }

    func get<T: Decodable>(_ type: T.Type, _ resource: String, query: [URLQueryItem] = []) async throws -> T {
        try JSONDecoder().decode(T.self, from: try await data("GET", url: url(resource, query: query)))
    }

    /// POST/PUT/PATCH/DELETE with an optional JSON body; answers the response (null when empty).
    @discardableResult
    func send(_ method: String, _ resource: String, query: [URLQueryItem] = [], json: JSONValue? = nil) async throws -> JSONValue {
        let body = try json?.encoded()
        let out = try await data(method, url: url(resource, query: query), body: body,
                                 contentType: body == nil ? nil : "application/json")
        return (try? JSONValue.parse(out)) ?? .null
    }

    /// A form-encoded request (Bazarr's settings and task endpoints read form fields).
    @discardableResult
    func sendForm(_ method: String, _ resource: String, query: [URLQueryItem] = [], fields: [(String, String)]) async throws -> JSONValue {
        let body = Self.formBody(fields)
        let out = try await data(method, url: url(resource, query: query), body: body,
                                 contentType: "application/x-www-form-urlencoded")
        return (try? JSONValue.parse(out)) ?? .null
    }

    /// RFC 3986 unreserved characters, ASCII only (non-ASCII letters must be encoded too).
    static let unreserved = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")

    static func formBody(_ fields: [(String, String)]) -> Data {
        let allowed = Self.unreserved
        let text = fields.map { key, value in
            (key.addingPercentEncoding(withAllowedCharacters: allowed) ?? key) + "=" +
                (value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value)
        }.joined(separator: "&")
        return Data(text.utf8)
    }

    /// Starts a Servarr command (`POST command {name, …}`).
    @discardableResult
    func command(_ name: String, _ params: [String: JSONValue] = [:]) async throws -> JSONValue {
        var body = params
        body["name"] = .string(name)
        return try await send("POST", "command", json: .object(body))
    }

    /// Downloads a file (a backup) to a temporary file with the given name, for the share sheet.
    func download(_ url: URL, fileName: String) async throws -> URL {
        var req = URLRequest(url: url)
        req.timeoutInterval = 300
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let file: URL, response: URLResponse
        do {
            (file, response) = try await URLSession.shared.download(for: req)
        } catch {
            throw ArrError.network
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else {
            try? FileManager.default.removeItem(at: file)
            throw ArrError.http(status, nil)
        }
        let dir = FileManager.default.temporaryDirectory.appending(path: "arr-files", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let target = dir.appending(path: (fileName as NSString).lastPathComponent)
        try? FileManager.default.removeItem(at: target)
        try FileManager.default.moveItem(at: file, to: target)
        return target
    }
}

extension MediaClient {
    /// The *arr services the server knows about (admin only).
    func arrServices() async throws -> [ArrServiceStatus] {
        let response: ArrServicesResponse = try await get("arr/services")
        return response.services
    }

    func arr(_ service: ArrService, apiBase: String? = nil) -> ArrClient {
        ArrClient(media: self, service: service, apiBase: apiBase)
    }
}

extension URLQueryItem {
    /// Shorthand for query lists: `.q("page", 1)`.
    static func q(_ name: String, _ value: any CustomStringConvertible) -> URLQueryItem {
        URLQueryItem(name: name, value: value.description)
    }
}
