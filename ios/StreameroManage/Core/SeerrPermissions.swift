import Foundation

/// A permission bit of a Seerr user, as Seerr's public API documents the `permissions` mask.
struct SeerrPermission: Hashable, Sendable, Identifiable {
    let bit: Int64
    /// The localization key of the label ("manage.perm.admin").
    let key: String

    var id: Int64 { bit }
    /// The bit's position (2 → 1).
    var position: Int { bit.trailingZeroBitCount }
}

/// Groups the known bits for the editor and keeps every other bit untouched.
enum SeerrPermissions {
    static let admin = SeerrPermission(bit: 2, key: "manage.perm.admin")

    struct Group: Identifiable, Sendable {
        let titleKey: String
        let permissions: [SeerrPermission]
        var id: String { titleKey }
    }

    static let groups: [Group] = [
        Group(titleKey: "manage.perm.group.admin", permissions: [
            admin,
            SeerrPermission(bit: 4, key: "manage.perm.manage_settings"),
            SeerrPermission(bit: 8, key: "manage.perm.manage_users"),
            SeerrPermission(bit: 16, key: "manage.perm.manage_requests"),
            SeerrPermission(bit: 1_048_576, key: "manage.perm.manage_issues"),
        ]),
        Group(titleKey: "manage.perm.group.request", permissions: [
            SeerrPermission(bit: 32, key: "manage.perm.request"),
            SeerrPermission(bit: 262_144, key: "manage.perm.request_movie"),
            SeerrPermission(bit: 524_288, key: "manage.perm.request_tv"),
            SeerrPermission(bit: 8_192, key: "manage.perm.request_advanced"),
            SeerrPermission(bit: 16_384, key: "manage.perm.request_view"),
        ]),
        Group(titleKey: "manage.perm.group.request_4k", permissions: [
            SeerrPermission(bit: 1_024, key: "manage.perm.request_4k"),
            SeerrPermission(bit: 2_048, key: "manage.perm.request_4k_movie"),
            SeerrPermission(bit: 4_096, key: "manage.perm.request_4k_tv"),
        ]),
        Group(titleKey: "manage.perm.group.auto_approve", permissions: [
            SeerrPermission(bit: 128, key: "manage.perm.auto_approve"),
            SeerrPermission(bit: 256, key: "manage.perm.auto_approve_movie"),
            SeerrPermission(bit: 512, key: "manage.perm.auto_approve_tv"),
            SeerrPermission(bit: 32_768, key: "manage.perm.auto_approve_4k"),
            SeerrPermission(bit: 65_536, key: "manage.perm.auto_approve_4k_movie"),
            SeerrPermission(bit: 131_072, key: "manage.perm.auto_approve_4k_tv"),
        ]),
        Group(titleKey: "manage.perm.group.auto_request", permissions: [
            SeerrPermission(bit: 8_388_608, key: "manage.perm.auto_request"),
            SeerrPermission(bit: 16_777_216, key: "manage.perm.auto_request_movie"),
            SeerrPermission(bit: 33_554_432, key: "manage.perm.auto_request_tv"),
        ]),
        Group(titleKey: "manage.perm.group.other", permissions: [
            SeerrPermission(bit: 2_097_152, key: "manage.perm.view_issues"),
            SeerrPermission(bit: 4_194_304, key: "manage.perm.create_issues"),
            SeerrPermission(bit: 67_108_864, key: "manage.perm.recent_view"),
            SeerrPermission(bit: 134_217_728, key: "manage.perm.watchlist_view"),
            SeerrPermission(bit: 64, key: "manage.perm.vote"),
        ]),
    ]

    static let known: [SeerrPermission] = groups.flatMap(\.permissions)

    static var knownMask: Int64 { known.reduce(0) { $0 | $1.bit } }

    static func has(_ mask: Int64, _ permission: SeerrPermission) -> Bool { mask & permission.bit != 0 }

    static func setting(_ mask: Int64, _ permission: SeerrPermission, _ on: Bool) -> Int64 {
        on ? mask | permission.bit : mask & ~permission.bit
    }

    /// Bits set in the mask that the app has no label for, by position ("Other (bit 28)").
    static func unknownPositions(_ mask: Int64) -> [Int] {
        let rest = mask & ~knownMask
        return (0..<63).filter { rest & (Int64(1) << $0) != 0 }
    }

    /// The known permissions that are on.
    static func enabled(_ mask: Int64) -> [SeerrPermission] { known.filter { has(mask, $0) } }

    /// Rebuilds a mask from switched-on known permissions plus the original's unknown bits;
    /// the inverse of `enabled` for any mask.
    static func mask(enabled: [SeerrPermission], keepingUnknownFrom original: Int64) -> Int64 {
        enabled.reduce(original & ~knownMask) { $0 | $1.bit }
    }

    /// Whether saving would take administrator rights away.
    static func removesAdmin(from before: Int64, to after: Int64) -> Bool {
        has(before, admin) && !has(after, admin)
    }
}

/// Seerr's per-user request limits (`user/{id}/settings/main`): nil limit means the global default.
struct SeerrQuota: Equatable, Sendable {
    var movieLimit: Int?
    var movieDays: Int?
    var tvLimit: Int?
    var tvDays: Int?

    static let keys = ["movieQuotaLimit", "movieQuotaDays", "tvQuotaLimit", "tvQuotaDays"]

    init(movieLimit: Int? = nil, movieDays: Int? = nil, tvLimit: Int? = nil, tvDays: Int? = nil) {
        self.movieLimit = movieLimit
        self.movieDays = movieDays
        self.tvLimit = tvLimit
        self.tvDays = tvDays
    }

    init(_ json: JSONValue) {
        movieLimit = json["movieQuotaLimit"].int
        movieDays = json["movieQuotaDays"].int
        tvLimit = json["tvQuotaLimit"].int
        tvDays = json["tvQuotaDays"].int
    }

    /// Whether the settings object has quota keys at all (older versions may not).
    static func isPresent(in json: JSONValue) -> Bool {
        guard let o = json.object else { return false }
        return keys.contains { o.keys.contains($0) }
    }

    /// The settings object with these limits; every other key stays as it was.
    func applied(to json: JSONValue) -> JSONValue {
        var out = json
        out["movieQuotaLimit"] = movieLimit.map { .number($0) } ?? .null
        out["movieQuotaDays"] = movieDays.map { .number($0) } ?? .null
        out["tvQuotaLimit"] = tvLimit.map { .number($0) } ?? .null
        out["tvQuotaDays"] = tvDays.map { .number($0) } ?? .null
        return out
    }
}
