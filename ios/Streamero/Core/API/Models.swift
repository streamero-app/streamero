import Foundation

// Hand-written Codable models mirroring api/openapi.yaml (the source of truth); the plain
// URLSession client in MediaClient uses them so the API surface stays small and tolerant.
// Rule for request bodies: a field the spec lists as required is non-optional here, so the
// encoder always writes it (even false or 0). Optional fields stay `Optional` and are
// omitted when nil. Durations and positions are integer milliseconds.

struct UserPermissions: Codable, Sendable, Equatable {
    var allLibraries: Bool
    var download: Bool
    var videoTranscoding: Bool
    var audioTranscoding: Bool
    var remux: Bool
    var liveTv: Bool
    var manageLiveTv: Bool
    var syncPlay: Bool
    var deleteMedia: Bool
    var manageCollections: Bool
    var editSubtitles: Bool
    var remoteControl: Bool
    var maxSessions: Int
}

struct Profile: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var color: String
    var hasPin: Bool
    var isKid: Bool
    var maxRating: Int?
    var hasAvatar: Bool?
}

struct User: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var username: String
    var isAdmin: Bool
    var disabled: Bool
    var totpEnabled: Bool
    var permissions: UserPermissions
    var currentProfile: Profile?
    var displayName: String?
    var email: String?
    var language: String?
    var hasAvatar: Bool?
    var createdAt: String
    var lastLoginAt: String?
}

struct UpdateMe: Codable, Sendable, Equatable {
    var displayName: String?
    var email: String?
    var language: String?
}

struct ScanStatus: Codable, Sendable, Equatable {
    var state: String
    var lastScanAt: String?
    var error: String?
    var filesSeen: Int?
}

struct Library: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var kind: String
    var paths: [String]
    var itemCount: Int
    var scan: ScanStatus
}

struct LibraryInput: Codable, Sendable, Equatable {
    var name: String
    var kind: String
    var paths: [String]
}

struct ItemSummary: Codable, Sendable, Equatable, Identifiable, Hashable {
    var id: String
    var kind: String
    var title: String
    var year: Int?
    var seasonNumber: Int?
    var episodeNumber: Int?
    var durationMs: Int64?
    var addedAt: String
    var hasPoster: Bool
    var hasBackdrop: Bool?
    var hasLogo: Bool?
    var colors: [String]?
    var seriesId: String?
    var seriesTitle: String?
    var rating: Double?
    var parentId: String?
    var artist: String?
    var album: String?
    var videoHeight: Int?
    var hdr: String?
    var positionMs: Int64?
    var played: Bool?
    var favorite: Bool?
    // Books, audiobooks and photos.
    var authors: [String]?
    var seriesName: String?
    var seriesIndex: Double?
    var bookFormat: String?
    var progression: Double?
    var takenAt: String?
    var width: Int?
    var height: Int?
    var childCount: Int?

    var progress: Double {
        guard let d = durationMs, d > 0, let p = positionMs, p > 0 else { return 0 }
        return min(1, max(0, Double(p) / Double(d)))
    }
}

struct ItemPage: Codable, Sendable, Equatable {
    var items: [ItemSummary]
    var total: Int
}

struct AudioStream: Codable, Sendable, Equatable, Hashable {
    var codec: String
    var language: String?
    var channels: Int?
    var title: String?
}

struct SubtitleStream: Codable, Sendable, Equatable, Hashable {
    var codec: String
    var language: String?
    var forced: Bool?
    var external: Bool
}

struct MediaFile: Codable, Sendable, Equatable, Identifiable, Hashable {
    var id: String
    var path: String
    var label: String?
    var size: Int64
    var container: String
    var durationMs: Int64?
    var width: Int?
    var height: Int?
    var videoCodec: String?
    var hdr: String?
    var audio: [AudioStream]?
    var subtitles: [SubtitleStream]?
}

struct Person: Codable, Sendable, Equatable {
    var name: String
    var role: String?
    var imageUrl: String?
}

struct MediaCollection: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var overview: String?
    var itemCount: Int
    var hasPoster: Bool
    var hasBackdrop: Bool
    var auto: Bool
}

struct Chapter: Codable, Sendable, Equatable, Hashable {
    var startMs: Int64
    var title: String
}

struct TrickplayInfo: Codable, Sendable, Equatable {
    var intervalMs: Int
    var width: Int
    var height: Int
    var columns: Int
    var rows: Int
    var count: Int
    var sheetUrls: [String]
}

struct ItemDetail: Codable, Sendable, Equatable {
    var item: ItemSummary
    var libraryId: String
    var parentId: String?
    var overview: String?
    var tagline: String?
    var originalTitle: String?
    var genres: [String]
    var studios: [String]?
    var officialRating: String?
    var premiereDate: String?
    var cast: [Person]
    var crew: [Person]
    var hasBackdrop: Bool
    var hasLogo: Bool?
    var files: [MediaFile]
    var children: [ItemSummary]
    var collections: [MediaCollection]?
    var chapters: [Chapter]?
    var trickplay: TrickplayInfo?
    var providerIds: [String: String]
    var metadataError: String?
    var book: BookDetails?
    var photo: PhotoDetails?
}

struct DeviceProfile: Codable, Sendable, Equatable {
    var containers: [String]
    var videoCodecs: [String]
    var audioCodecs: [String]
    var hls: Bool
    var maxHeight: Int?
    var maxBitrate: Int?
}

struct PlaybackRequest: Codable, Sendable, Equatable {
    var device: DeviceProfile
    var fileId: String?
    var audioIndex: Int?
    var startMs: Int64?
}

struct PlaybackSubtitle: Codable, Sendable, Equatable, Hashable {
    var index: Int
    var language: String?
    var forced: Bool?
    var external: Bool
    var url: String
}

struct MediaSegment: Codable, Sendable, Equatable {
    var kind: String
    var startMs: Int64
    var endMs: Int64
}

struct PlaybackInfo: Codable, Sendable, Equatable {
    var fileId: String
    var method: String
    var url: String
    var startMs: Int64
    var durationMs: Int64?
    var audio: [AudioStream]
    var audioIndex: Int?
    var subtitles: [PlaybackSubtitle]
    var subtitleDefault: Int?
    var chapters: [Chapter]?
    var segments: [MediaSegment]?
    var trickplay: TrickplayInfo?
    var reason: String?
}

struct ProgressReport: Codable, Sendable, Equatable {
    var positionMs: Int64
    var durationMs: Int64?
    var played: Bool?
}

struct UserDataUpdate: Codable, Sendable, Equatable {
    var favorite: Bool?
    var played: Bool?
}

/// Per-profile playback preferences; PUT replaces the whole document.
struct Preferences: Codable, Sendable, Equatable {
    var audioLanguage: String?
    var subtitleLanguage: String?
    var subtitleMode: String
    var autoplayNext: Bool
    var skipBackSeconds: Int?
    var skipForwardSeconds: Int?
    var maxBitrateKbps: Int?
    var maxHeight: Int?
    var upNextSeconds: Int?
    var skipIntro: String?
    var skipCredits: String?
    var subtitleSize: String?
    var subtitleColor: String?
    var subtitleBackground: String?
    var subtitlePosition: Int?
    var homeSections: [String]?
}

struct DiscoverTitle: Codable, Sendable, Equatable, Identifiable {
    var tmdbId: Int
    var mediaType: String
    var title: String
    var year: Int?
    var overview: String?
    var posterUrl: String?
    var backdropUrl: String?
    var rating: Double?
    var status: String
    var itemId: String?

    var id: String { "\(mediaType)\(tmdbId)" }
}

struct DiscoverPage: Codable, Sendable, Equatable {
    var configured: Bool
    var canRequest: Bool
    var titles: [DiscoverTitle]
    var page: Int
    var totalPages: Int
}

struct TitleRequest: Codable, Sendable, Equatable {
    var tmdbId: Int
    var mediaType: String
}

struct TitleRequestResult: Codable, Sendable, Equatable {
    var status: String
}

struct UpcomingRelease: Codable, Sendable, Equatable {
    var kind: String
    var title: String
    var seriesTitle: String?
    var season: Int?
    var episode: Int?
    var date: String
    var overview: String?
    var posterUrl: String?
    var downloaded: Bool
}

struct Enhancements: Codable, Sendable, Equatable {
    var qualityTags = true
    var randomButton = true
    var pauseScreen = true
    var arrLinks = true
    var discoverInSearch = true
    var removeFromContinue = true
    var shortcutsHelp = true
    var autoPip = false
}

// MARK: Administration

struct NowPlaying: Codable, Sendable, Equatable {
    var key: String
    var user: String
    var profile: String?
    var device: String
    var client: String?
    var itemId: String
    var title: String
    var method: String
    var reason: String?
    var positionMs: Int64
    var durationMs: Int64?
}

struct StorageUsage: Codable, Sendable, Equatable {
    var kind: String
    var path: String
    var totalBytes: Int64
    var freeBytes: Int64
}

struct ActivityEntry: Codable, Sendable, Equatable {
    var at: String
    var user: String?
    var action: String
}

struct DashboardServer: Codable, Sendable, Equatable {
    var name: String
    var version: String
    var os: String?
    var startedAt: String?
}

struct Dashboard: Codable, Sendable, Equatable {
    var server: DashboardServer
    var storage: [StorageUsage]
    var nowPlaying: [NowPlaying]
    var activeTranscodes: Int
    var activity: [ActivityEntry]
}

struct SessionCommand: Codable, Sendable, Equatable {
    var kind: String
    var text: String?
}

struct TaskInfo: Codable, Sendable, Equatable {
    var key: String
    var running: Bool
    var lastRunAt: String?
    var lastState: String?
    var interval: String?
}

struct LogEntry: Codable, Sendable, Equatable {
    var time: String
    var level: String
    var message: String
    var source: String
    var attrs: String?
}

struct ActivityPage: Codable, Sendable, Equatable {
    var entries: [ActivityEntry]
    var total: Int
}

struct CreateUser: Codable, Sendable, Equatable {
    var username: String
    var password: String
    var isAdmin: Bool
}

struct UpdateUser: Codable, Sendable, Equatable {
    var isAdmin: Bool?
    var disabled: Bool?
    var password: String?
    var resetTwoFactor: Bool?
}

struct SessionInfo: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var userId: String
    var username: String
    var deviceName: String
    var client: String
    var createdAt: String
    var lastSeenAt: String
    var current: Bool
}

struct ApiKey: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var prefix: String
    var createdBy: String
    var createdAt: String
    var lastUsedAt: String?
}

struct CreatedApiKey: Codable, Sendable, Equatable {
    var key: String
    var apiKey: ApiKey
}

struct CreateApiKey: Codable, Sendable, Equatable {
    var name: String
}

struct CreateInvite: Codable, Sendable, Equatable {
    var expiresInHours: Int
    var maxUses: Int
    var note: String?
}

struct Invite: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var note: String
    var createdBy: String
    var createdAt: String
    var expiresAt: String
    var maxUses: Int
    var uses: Int
}

struct CreatedInvite: Codable, Sendable, Equatable {
    var token: String
    var invite: Invite
}

struct CollectionDetail: Codable, Sendable, Equatable {
    var collection: MediaCollection
    var items: [ItemSummary]
}

struct Playlist: Codable, Sendable, Equatable, Identifiable {
    var id: String
    var name: String
    var itemCount: Int
    var durationMs: Int64
    var `public`: Bool
    var mine: Bool
    var coverItemIds: [String]?
}

struct PlaylistDetail: Codable, Sendable, Equatable {
    var playlist: Playlist
    var items: [ItemSummary]
}

struct ServerSettings: Codable, Sendable, Equatable {
    var serverName: String
}

struct MetadataSettings: Codable, Sendable, Equatable {
    var tmdbConfigured: Bool
    var language: String
    var country: String
}

struct MetadataSettingsInput: Codable, Sendable, Equatable {
    var language: String
    var country: String
    var tmdbApiKey: String?
    var removeTmdbKey: Bool?
}

struct EncodingSettings: Codable, Sendable, Equatable {
    var hardwareAcceleration: String
    var vaapiDevice: String?
    var preset: String
    var quality: Int
    var audioBitrateKbps: Int
    var segmentSeconds: Int
    var maxSessions: Int
    var toneMapping: Bool
}

struct ResumeSettings: Codable, Sendable, Equatable {
    var minResumePercent: Int
    var maxResumePercent: Int
    var minResumeDurationSeconds: Int
}

struct BrandingSettings: Codable, Sendable, Equatable {
    var loginDisclaimer: String
    var customCss: String
}

struct RequestSettings: Codable, Sendable, Equatable {
    var seerrUrl: String?
    var seerrConfigured: Bool
    var usersCanRequest: Bool
    var radarrUrl: String?
    var radarrConfigured: Bool
    var sonarrUrl: String?
    var sonarrConfigured: Bool
    var bazarrUrl: String?
    var bazarrConfigured: Bool?
}

struct RequestSettingsInput: Codable, Sendable, Equatable {
    var usersCanRequest: Bool
    var seerrUrl: String?
    var seerrApiKey: String?
    var radarrUrl: String?
    var radarrApiKey: String?
    var sonarrUrl: String?
    var sonarrApiKey: String?
    var bazarrUrl: String?
    var bazarrApiKey: String?
}

struct ConnectionCheck: Codable, Sendable, Equatable {
    var app: String
    var ok: Bool
    var version: String?
    var error: String?
}

struct SubtitleOffer: Codable, Sendable, Equatable, Identifiable {
    var provider: String
    var language: String
    var forced: Bool
    var hearingImpaired: Bool
    var score: Double
    var release: [String]?
    var uploader: String?
    var subtitle: String
    var originalFormat: Bool

    var id: String { provider + "|" + subtitle }
}

struct SubtitleSearch: Codable, Sendable, Equatable {
    var configured: Bool
    var offers: [SubtitleOffer]
}

struct APIErrorBody: Codable, Sendable {
    var code: String
    var message: String
}

/// Who may see administration and who may fetch subtitles; pure so it can be tested.
enum Access {
    static func isAdmin(_ user: User?) -> Bool {
        guard let user else { return false }
        return user.isAdmin && user.currentProfile?.isKid != true
    }

    static func isKid(_ user: User?) -> Bool {
        user?.currentProfile?.isKid == true
    }

    /// Same rule as the web and Android clients.
    static func canFindSubtitles(_ user: User?) -> Bool {
        guard let user else { return false }
        return (user.isAdmin || user.permissions.editSubtitles) && user.currentProfile?.isKid != true
    }
}
