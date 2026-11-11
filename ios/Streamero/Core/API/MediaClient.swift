import Foundation

/// A plain JSON client for one Streamero server, signed in with the session token.
/// Signed stream URLs need no token; everything under /api/v1 gets it. Mirrors the Android
/// client (`api/Api.kt`); the generated client stays in use for the sign-in calls.
struct MediaClient: Sendable {
    let baseURL: URL
    let token: String

    /// Encodes a request body: required fields always (even false/0), nil optionals never.
    static func encode<B: Encodable>(_ body: B) throws -> Data {
        try JSONEncoder().encode(body)
    }

    private func apiURL(_ path: String, query: [String: String?]) -> URL {
        var url = ServerURL.apiBase(baseURL).appending(path: path)
        let items = query.compactMap { key, value in value.map { URLQueryItem(name: key, value: $0) } }
        if !items.isEmpty { url.append(queryItems: items.sorted { $0.name < $1.name }) }
        return url
    }

    /// The URL of an API path; tests use it to check query building.
    func url(path: String, query: [String: String?] = [:]) -> URL { apiURL(path, query: query) }

    private func perform(_ method: String, _ path: String, query: [String: String?] = [:],
                         body: Data? = nil, contentType: String? = nil) async throws -> Data {
        var req = URLRequest(url: apiURL(path, query: query))
        req.httpMethod = method
        req.timeoutInterval = 30
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let contentType { req.setValue(contentType, forHTTPHeaderField: "Content-Type") }
        req.httpBody = body
        let (data, response) = try await URLSession.shared.data(for: req)
        guard let http = response as? HTTPURLResponse else { throw APIError(status: 0, code: "bad_response") }
        guard (200..<300).contains(http.statusCode) else {
            let err = try? JSONDecoder().decode(APIErrorBody.self, from: data)
            throw APIError(status: http.statusCode, code: err?.code ?? "http_\(http.statusCode)", message: err?.message)
        }
        return data
    }

    func get<T: Decodable>(_ path: String, query: [String: String?] = [:]) async throws -> T {
        try JSONDecoder().decode(T.self, from: try await perform("GET", path, query: query))
    }

    func post<B: Encodable, T: Decodable>(_ path: String, _ body: B) async throws -> T {
        try JSONDecoder().decode(T.self, from: try await perform("POST", path, body: try Self.encode(body), contentType: "application/json"))
    }

    func put<B: Encodable, T: Decodable>(_ path: String, _ body: B) async throws -> T {
        try JSONDecoder().decode(T.self, from: try await perform("PUT", path, body: try Self.encode(body), contentType: "application/json"))
    }

    func patch<B: Encodable, T: Decodable>(_ path: String, _ body: B) async throws -> T {
        try JSONDecoder().decode(T.self, from: try await perform("PATCH", path, body: try Self.encode(body), contentType: "application/json"))
    }

    func send<B: Encodable>(_ method: String, _ path: String, _ body: B) async throws {
        _ = try await perform(method, path, body: try Self.encode(body), contentType: "application/json")
    }

    func send(_ method: String, _ path: String) async throws {
        _ = try await perform(method, path)
    }

    // MARK: Account

    func me() async throws -> User { try await get("auth/me") }
    func updateMe(_ update: UpdateMe) async throws -> User { try await put("auth/me", update) }

    func uploadAvatar(_ data: Data) async throws {
        _ = try await perform("PUT", "auth/me/avatar", body: data, contentType: "application/octet-stream")
    }

    func removeAvatar() async throws { try await send("DELETE", "auth/me/avatar") }

    func changePassword(current: String, new: String) async throws {
        try await send("POST", "auth/password", ["currentPassword": current, "newPassword": new])
    }

    func profiles() async throws -> [Profile] { try await get("profiles") }

    /// Switches this device to a profile (PIN when it has one).
    func selectProfile(_ id: String, pin: String?) async throws {
        struct Body: Encodable { var pin: String? }
        try await send("POST", "profiles/\(id)/select", Body(pin: pin))
    }

    /// Back to the account's main profile (password only needed when leaving a kids profile).
    func selectMainProfile(password: String = "") async throws {
        try await send("POST", "profiles/main/select", ["password": password])
    }

    // MARK: Browse

    func libraries() async throws -> [Library] { try await get("libraries") }

    func items(library id: String, query: [String: String?]) async throws -> ItemPage {
        try await get("libraries/\(id)/items", query: query)
    }

    func genres(library id: String) async throws -> [String] { try await get("libraries/\(id)/genres") }
    func item(_ id: String) async throws -> ItemDetail { try await get("items/\(id)") }
    func continueWatching() async throws -> [ItemSummary] { try await get("home/continue") }
    func latest() async throws -> [ItemSummary] { try await get("home/latest") }
    func nextUp() async throws -> [ItemSummary] { try await get("home/next-up") }
    func favorites() async throws -> [ItemSummary] { try await get("home/favorites") }
    func search(_ q: String) async throws -> [ItemSummary] { try await get("search", query: ["q": q]) }

    /// The episode after this one (nil after the last one).
    func nextEpisode(_ id: String) async throws -> ItemSummary? {
        do {
            let next: ItemSummary = try await get("items/\(id)/next")
            return next
        } catch let error as APIError where error.status == 404 {
            return nil
        }
    }

    /// Favorite / watched for the current profile.
    func setUserData(item id: String, favorite: Bool? = nil, played: Bool? = nil) async throws {
        try await send("PUT", "items/\(id)/user-data", UserDataUpdate(favorite: favorite, played: played))
    }

    func collections() async throws -> [MediaCollection] { try await get("collections") }
    func collection(_ id: String) async throws -> CollectionDetail { try await get("collections/\(id)") }
    func playlists() async throws -> [Playlist] { try await get("playlists") }
    func playlist(_ id: String) async throws -> PlaylistDetail { try await get("playlists/\(id)") }

    // MARK: Playback

    /// What iOS plays natively: AVPlayer handles MP4/MOV, HEVC (incl. HDR) and HLS with fMP4.
    static let device = DeviceProfile(
        containers: ["mp4", "m4v", "mov"],
        videoCodecs: ["h264", "hevc", "hdr"],
        audioCodecs: ["aac", "ac3", "eac3", "mp3", "alac", "flac"],
        hls: true)

    /// The video profile with the profile's streaming-quality limits (kbps to bits/s).
    static func device(preferences: Preferences?) -> DeviceProfile {
        var profile = device
        profile.maxHeight = preferences?.maxHeight
        profile.maxBitrate = preferences?.maxBitrateKbps.map { $0 * 1000 }
        return profile
    }

    /// What the phone plays for music without help (the server converts anything else).
    static let audioProfile = DeviceProfile(
        containers: ["mp3", "flac", "mp4", "aac", "m4a", "wav"],
        videoCodecs: [],
        audioCodecs: ["mp3", "flac", "aac", "alac"],
        hls: true)

    func startPlayback(item id: String, startMs: Int64? = nil, audioIndex: Int? = nil,
                       fileId: String? = nil, profile: DeviceProfile = MediaClient.device) async throws -> PlaybackInfo {
        try await post("items/\(id)/playback", PlaybackRequest(device: profile, fileId: fileId, audioIndex: audioIndex, startMs: startMs))
    }

    func reportProgress(item id: String, positionMs: Int64, durationMs: Int64?) async {
        try? await send("PUT", "items/\(id)/progress", ProgressReport(positionMs: positionMs, durationMs: durationMs))
    }

    func preferences() async throws -> Preferences { try await get("me/preferences") }
    func savePreferences(_ p: Preferences) async throws -> Preferences { try await put("me/preferences", p) }
    func commands() async throws -> [SessionCommand] { try await get("me/commands") }

    /// WebVTT text for an already signed URL.
    func text(from url: URL) async throws -> String {
        let (data, response) = try await URLSession.shared.data(from: url)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw APIError(status: 0, code: "bad_subtitles") }
        return String(decoding: data, as: UTF8.self)
    }

    // MARK: Offline downloads

    func createDownloads(_ request: DownloadRequestBody) async throws -> [DownloadJob] { try await post("downloads", request) }
    func download(_ id: String) async throws -> DownloadJob { try await get("downloads/\(id)") }
    func deleteDownload(_ id: String) async throws { try await send("DELETE", "downloads/\(id)") }
    func syncDownloads(_ request: OfflineSyncRequest) async throws -> OfflineSyncResponse { try await post("downloads/sync", request) }

    /// Raw bytes of an API URL that needs the session token (artwork for the offline library).
    func authorizedData(_ url: URL) async throws -> Data {
        var req = URLRequest(url: url)
        req.timeoutInterval = 30
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await URLSession.shared.data(for: req)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw APIError(status: status, code: "http_\(status)") }
        return data
    }

    /// Raw bytes of an already signed URL (subtitle sidecars).
    func signedData(_ url: URL) async throws -> Data {
        let (data, response) = try await URLSession.shared.data(from: url)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw APIError(status: status, code: "http_\(status)") }
        return data
    }

    // MARK: Watch together

    func syncGroups() async throws -> [SyncGroup] { try await get("syncplay/groups") }
    func syncGroup(_ id: String) async throws -> SyncGroup { try await get("syncplay/groups/\(id)") }
    func createSyncGroup(_ req: CreateSyncGroupRequest) async throws -> SyncGroup { try await post("syncplay/groups", req) }

    func joinSyncGroup(_ id: String) async throws -> SyncGroup {
        try JSONDecoder().decode(SyncGroup.self, from: try await perform("POST", "syncplay/groups/\(id)/join"))
    }

    /// The group this session is in; nil when the server answers 204 (not in a group).
    func mySyncGroup() async throws -> SyncGroup? {
        let data = try await perform("GET", "syncplay/me")
        return data.isEmpty ? nil : try JSONDecoder().decode(SyncGroup.self, from: data)
    }

    func leaveSyncGroup() async throws { try await send("POST", "syncplay/leave") }

    // MARK: Discover

    func discover(_ q: String? = nil, page: Int = 1) async throws -> DiscoverPage {
        try await get("discover", query: ["q": q.flatMap { $0.isEmpty ? nil : $0 }, "page": String(page)])
    }

    func request(_ title: DiscoverTitle) async throws -> TitleRequestResult {
        try await post("discover/request", TitleRequest(tmdbId: title.tmdbId, mediaType: title.mediaType))
    }

    func upcoming() async throws -> [UpcomingRelease] { try await get("upcoming", query: ["days": "30"]) }
    func enhancements() async throws -> Enhancements { try await get("enhancements") }

    // MARK: Subtitles (Bazarr)

    func searchSubtitles(item id: String) async throws -> SubtitleSearch { try await get("items/\(id)/subtitles/search") }

    func downloadSubtitle(item id: String, offer: SubtitleOffer) async throws {
        try await send("POST", "items/\(id)/subtitles/download", offer)
    }

    func searchMissingSubtitles(item id: String) async throws { try await send("POST", "items/\(id)/subtitles/search-missing") }

    // MARK: Administration

    func dashboard() async throws -> Dashboard { try await get("dashboard") }
    func sendCommand(sessionKey: String, _ command: SessionCommand) async throws {
        try await send("POST", "sessions/\(sessionKey)/command", command)
    }
    func tasks() async throws -> [TaskInfo] { try await get("tasks") }
    func runTask(_ key: String) async throws { try await send("POST", "tasks/\(key)/run") }
    func logs(level: String, q: String?) async throws -> [LogEntry] {
        try await get("admin/logs", query: ["level": level, "q": q.flatMap { $0.isEmpty ? nil : $0 }, "limit": "300"])
    }
    func activity(offset: Int) async throws -> ActivityPage {
        try await get("admin/activity", query: ["limit": "50", "offset": String(offset)])
    }
    func users() async throws -> [User] { try await get("users") }
    func createUser(_ u: CreateUser) async throws -> User { try await post("users", u) }
    func updateUser(_ id: String, _ u: UpdateUser) async throws -> User { try await patch("users/\(id)", u) }
    func deleteUser(_ id: String) async throws { try await send("DELETE", "users/\(id)") }
    func sessions() async throws -> [SessionInfo] { try await get("sessions") }
    func revokeSession(_ id: String) async throws { try await send("DELETE", "sessions/\(id)") }
    func apiKeys() async throws -> [ApiKey] { try await get("apikeys") }
    func createApiKey(name: String) async throws -> CreatedApiKey { try await post("apikeys", CreateApiKey(name: name)) }
    func deleteApiKey(_ id: String) async throws { try await send("DELETE", "apikeys/\(id)") }
    func invites() async throws -> [Invite] { try await get("invites") }
    func createInvite(_ c: CreateInvite) async throws -> CreatedInvite { try await post("invites", c) }
    func deleteInvite(_ id: String) async throws { try await send("DELETE", "invites/\(id)") }
    func createLibrary(_ input: LibraryInput) async throws -> Library { try await post("libraries", input) }
    func deleteLibrary(_ id: String) async throws { try await send("DELETE", "libraries/\(id)") }
    func scanLibrary(_ id: String) async throws { try await send("POST", "libraries/\(id)/scan") }
    func refreshLibraryMetadata(_ id: String) async throws { try await send("POST", "libraries/\(id)/refresh-metadata") }
    func serverSettings() async throws -> ServerSettings { try await get("settings/server") }
    func saveServerSettings(_ s: ServerSettings) async throws -> ServerSettings { try await put("settings/server", s) }
    func metadataSettings() async throws -> MetadataSettings { try await get("settings/metadata") }
    func saveMetadataSettings(_ s: MetadataSettingsInput) async throws -> MetadataSettings { try await put("settings/metadata", s) }
    func encoding() async throws -> EncodingSettings { try await get("settings/encoding") }
    func saveEncoding(_ s: EncodingSettings) async throws -> EncodingSettings { try await put("settings/encoding", s) }
    func resume() async throws -> ResumeSettings { try await get("settings/resume") }
    func saveResume(_ s: ResumeSettings) async throws -> ResumeSettings { try await put("settings/resume", s) }
    func branding() async throws -> BrandingSettings { try await get("settings/branding") }
    func saveBranding(_ s: BrandingSettings) async throws -> BrandingSettings { try await put("settings/branding", s) }
    func requestSettings() async throws -> RequestSettings { try await get("settings/requests") }
    func saveRequestSettings(_ s: RequestSettingsInput) async throws -> RequestSettings { try await put("settings/requests", s) }
    func testRequestSettings() async throws -> [ConnectionCheck] {
        try JSONDecoder().decode([ConnectionCheck].self, from: try await perform("POST", "settings/requests/test"))
    }
    func saveEnhancements(_ e: Enhancements) async throws -> Enhancements { try await put("enhancements", e) }

    /// Approves a Quick Connect code shown on another device (TV, browser).
    func authorizeQuickConnect(code: String) async throws -> String {
        struct Approved: Decodable { var deviceName: String }
        let approved: Approved = try await post("quickconnect/authorize", ["code": code])
        return approved.deviceName
    }

    // MARK: URLs

    /// Absolute URL for a server-relative link (signed stream URLs, images).
    func absolute(_ path: String) -> URL? {
        URL(string: path, relativeTo: baseURL)?.absoluteURL
    }

    func imageURL(item id: String, type: String) -> URL? {
        absolute("/api/v1/items/\(id)/images/\(type)")
    }

    func avatarURL(owner id: String) -> URL? { absolute("/api/v1/avatars/\(id)") }
    func collectionImageURL(_ id: String, type: String) -> URL? { absolute("/api/v1/collections/\(id)/images/\(type)") }
    /// Absolute URL of an invite link on this server.
    func inviteLink(token: String) -> URL? { absolute("/invite/\(token)") }
}
