import Foundation
import Observation

/// What the player cover is showing, with the choices made on the item page.
struct PlayTarget: Identifiable, Hashable {
    let id: String
    var start: Int64? = nil
    var file: String? = nil
    var audio: Int? = nil
    var sub: Int? = nil
}

struct LibraryRoute: Hashable {
    let id: String
    let name: String
    let kind: String
}

/// Books or audiobooks of one author or series.
struct BookListRoute: Hashable {
    let libraryId: String
    /// "book" or "audiobook".
    let kind: String
    let title: String
    var author: String? = nil
    var series: String? = nil
}

/// The photos, videos and sub-albums of one photo album.
struct PhotoAlbumRoute: Hashable {
    let libraryId: String
    let id: String
    let title: String
}

/// Every screen reachable from a tab's navigation stack.
enum Route: Hashable {
    case item(String)
    case library(LibraryRoute)
    case books(BookListRoute)
    case photoAlbum(PhotoAlbumRoute)
    case favorites
    case collections
    case collection(String)
    case playlist(String)
    case settings(SettingsPage)
    case downloads
    case liveTv
    case liveChannel(LiveTvChannel)
}

/// Personal and administration settings; the admin pages also guard themselves.
enum SettingsPage: String, CaseIterable, Hashable {
    case account, playback, subtitles, home
    case dashboard, libraries, users, metadata, transcoding, requests, enhancements, tasks, activity, logs, devices, invites, apikeys, server, branding

    static let userPages: [SettingsPage] = [.account, .playback, .subtitles, .home]
    static let adminPages: [SettingsPage] = [.dashboard, .libraries, .users, .metadata, .transcoding, .requests, .enhancements, .tasks, .activity, .logs, .devices, .invites, .apikeys, .server, .branding]

    var isAdminOnly: Bool { Self.adminPages.contains(self) }
}

/// What the signed-in screens share: the server connection, who is signed in, music and the player.
@MainActor
@Observable
final class AppState {
    let server: SavedServer
    let client: MediaClient
    var me: User?
    var enhancements = Enhancements()
    var profiles: [Profile] = []
    /// Set by a screen to open the full-screen player.
    var playing: PlayTarget?
    /// Bumped when playback ends or the profile changes, so screens reload.
    var reloadTick = 0
    var showProfileGate = false
    let music: MusicPlayer
    /// Audiobooks; starting one stops the music and the other way round.
    let audiobook: AudiobookPlayer
    /// Watch-together membership (D-036).
    let together: SyncSession

    init(server: SavedServer, client: MediaClient) {
        self.server = server
        self.client = client
        let music = MusicPlayer(client: client)
        let audiobook = AudiobookPlayer(client: client)
        music.onStart = { [weak audiobook] in audiobook?.stop() }
        audiobook.onStart = { [weak music] in music?.stop() }
        self.music = music
        self.audiobook = audiobook
        self.together = SyncSession(client: client)
    }

    /// Whether this account may watch together.
    var canWatchTogether: Bool { isAdmin || me?.permissions.syncPlay == true }

    var isAdmin: Bool { Access.isAdmin(me) }
    var isKid: Bool { Access.isKid(me) }
    var canFindSubtitles: Bool { Access.canFindSubtitles(me) }

    var locale: Locale? {
        me?.language.map { Locale(identifier: $0) }
    }

    /// Reloads the account and the server's feature switches; returns the account or nil.
    @discardableResult
    func refreshMe() async -> User? {
        me = try? await client.me()
        enhancements = (try? await client.enhancements()) ?? Enhancements()
        Loc.apply(language: me?.language)
        return me
    }

    func play(_ target: PlayTarget) {
        audiobook.pause()
        playing = target
    }
}

/// Localization for strings built in code; SwiftUI views use the `locale` environment instead.
enum Loc {
    private static let key = "streamero.language"

    static func apply(language: String?) {
        guard let language else { return }
        UserDefaults.standard.set(language, forKey: key)
        // Also takes effect for system-localized text on the next launch.
        UserDefaults.standard.set([language], forKey: "AppleLanguages")
    }

    static func string(_ key: String) -> String {
        if let language = UserDefaults.standard.string(forKey: Self.key),
           let path = Bundle.main.path(forResource: language, ofType: "lproj"),
           let bundle = Bundle(path: path) {
            return bundle.localizedString(forKey: key, value: nil, table: nil)
        }
        return Bundle.main.localizedString(forKey: key, value: nil, table: nil)
    }
}
