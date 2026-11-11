import Foundation

// Live TV and DVR (D-041): models mirroring api/openapi.yaml, the client calls and the
// pure helpers the screens use (tested in LiveTVTests).

struct LiveTvProgram: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var channelId: String
    var channelName: String?
    var channelNumber: String?
    var title: String
    var subtitle: String?
    var description: String?
    var start: String
    var end: String
    var categories: [String]
    var category: String?
    var season: Int?
    var episode: Int?
    var year: Int?
    var isNew: Bool
    var isPremiere: Bool?
    var isRepeat: Bool?
    var isLive: Bool?
    var imageUrl: String?
    var recordingId: String?
    var recordingStatus: String?
    var seriesRuleId: String?
}

struct LiveTvChannel: Codable, Sendable, Equatable, Hashable, Identifiable {
    var id: String
    var number: String
    var name: String
    var group: String?
    var category: String?
    var logoUrl: String?
    var tunerId: String
    var favorite: Bool
    var hd: Bool?
    var now: LiveTvProgram?
    var next: LiveTvProgram?
}

struct LiveTvGuideRow: Codable, Sendable, Equatable {
    var channel: LiveTvChannel
    var programs: [LiveTvProgram]
}

struct LiveTvGuide: Codable, Sendable, Equatable {
    var start: String
    var end: String
    var rows: [LiveTvGuideRow]
}

struct LiveTvPlayRequest: Codable, Sendable, Equatable {
    var device: DeviceProfile
}

struct LiveTvPlayback: Codable, Sendable, Equatable {
    var channelId: String
    var url: String
    var method: String
    var reason: String?
    var timeshiftSeconds: Int
    var now: LiveTvProgram?
}

struct LiveTvRecording: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var channelId: String?
    var channelName: String?
    var programId: String?
    var seriesRuleId: String?
    var title: String
    var subtitle: String?
    var start: String
    var end: String
    var prePaddingSeconds: Int
    var postPaddingSeconds: Int
    var priority: Int
    var status: String
    var error: String?
    var createdAt: String
}

struct LiveTvRecordingCreate: Codable, Sendable, Equatable {
    var programId: String
    var channelId: String?
}

struct LiveTvSeriesRuleInput: Codable, Sendable, Equatable {
    var programId: String?
    var channelId: String?
    var newOnly: Bool
}

struct LiveTvSeriesRule: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var title: String
    var channelId: String?
    var newOnly: Bool
    var priority: Int
    var keepLast: Int
    var upcoming: Int
    var createdAt: String
}

/// Pure Live TV rules shared by the screens; no I/O so they can be unit tested.
enum LiveTV {
    /// Watching needs the liveTv permission (administrators may always); kids profiles too
    /// when the account allows it.
    static func canWatch(_ user: User?) -> Bool {
        guard let user else { return false }
        return user.isAdmin || user.permissions.liveTv
    }

    /// Scheduling recordings and series needs manageLiveTv (or an administrator), never in a kids profile.
    static func canRecord(_ user: User?) -> Bool {
        guard let user, user.currentProfile?.isKid != true else { return false }
        return user.isAdmin || user.permissions.manageLiveTv
    }

    /// Parses the server's RFC 3339 timestamps (with or without fractional seconds).
    static func date(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }

    /// How far a programme has run at `now`, 0…1 (0 before it starts or for bad times).
    static func progress(_ p: LiveTvProgram, now: Date) -> Double {
        guard let s = date(p.start), let e = date(p.end), e > s else { return 0 }
        return min(1, max(0, now.timeIntervalSince(s) / e.timeIntervalSince(s)))
    }

    /// Programmes that have not ended at `now`, in start order: the first is on now when it has started.
    static func upcoming(_ programs: [LiveTvProgram], now: Date) -> [LiveTvProgram] {
        programs
            .filter { (date($0.end) ?? .distantPast) > now }
            .sorted { (date($0.start) ?? .distantPast) < (date($1.start) ?? .distantPast) }
    }

    /// The programme on now and the one after it.
    static func nowNext(_ programs: [LiveTvProgram], now: Date) -> (now: LiveTvProgram?, next: LiveTvProgram?) {
        let rest = upcoming(programs, now: now)
        guard let first = rest.first else { return (nil, nil) }
        if (date(first.start) ?? .distantFuture) <= now {
            return (first, rest.dropFirst().first)
        }
        return (nil, first)
    }

    /// "20:15–21:00" in the device's time zone.
    static func timeRange(_ p: LiveTvProgram) -> String {
        guard let s = date(p.start), let e = date(p.end) else { return "" }
        let f = DateFormatter()
        f.dateStyle = .none
        f.timeStyle = .short
        return "\(f.string(from: s))–\(f.string(from: e))"
    }

    /// Favourites only when the filter is on; the server already sorts by channel number.
    static func filter(_ channels: [LiveTvChannel], favoritesOnly: Bool) -> [LiveTvChannel] {
        favoritesOnly ? channels.filter(\.favorite) : channels
    }

    /// Localization key of a recording state.
    static func statusKey(_ status: String) -> String { "livetv.status.\(status)" }

    /// Whether a programme can still be scheduled (no active recording and not over).
    static func canSchedule(_ p: LiveTvProgram, now: Date) -> Bool {
        if let status = p.recordingStatus, ["scheduled", "conflict", "recording"].contains(status) { return false }
        return (date(p.end) ?? .distantPast) > now
    }
}

extension MediaClient {
    func liveChannels(favorites: Bool = false) async throws -> [LiveTvChannel] {
        try await get("livetv/channels", query: ["favorites": favorites ? "true" : nil])
    }

    /// Programmes of one channel from now for `hours`.
    func liveGuide(hours: Int = 12) async throws -> LiveTvGuide {
        try await get("livetv/guide", query: ["hours": String(hours)])
    }

    func setLiveFavorite(channel id: String, _ on: Bool) async throws {
        try await send(on ? "PUT" : "DELETE", "livetv/channels/\(id)/favorite")
    }

    func playLive(channel id: String, profile: DeviceProfile = MediaClient.device) async throws -> LiveTvPlayback {
        try await post("livetv/channels/\(id)/play", LiveTvPlayRequest(device: profile))
    }

    func recordProgram(_ programId: String, channel: String?) async throws -> LiveTvRecording {
        try await post("livetv/recordings", LiveTvRecordingCreate(programId: programId, channelId: channel))
    }

    func recordSeries(_ programId: String, channel: String?, newOnly: Bool = true) async throws -> LiveTvSeriesRule {
        try await post("livetv/series-rules", LiveTvSeriesRuleInput(programId: programId, channelId: channel, newOnly: newOnly))
    }
}
