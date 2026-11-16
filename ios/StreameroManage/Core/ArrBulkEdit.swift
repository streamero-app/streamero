import Foundation

/// How a bulk edit changes the tags of the selected items (`applyTags` of the editor endpoints).
enum ArrTagMode: String, CaseIterable, Identifiable, Sendable {
    case add, remove, replace
    var id: String { rawValue }
}

/// A bulk edit of library items through `PUT {movie|series|artist}/editor`. Nil means
/// "leave as it is"; only what is set goes into the body.
struct ArrBulkEdit: Equatable, Sendable {
    var monitored: Bool?
    var qualityProfileId: Int?
    /// Lidarr only.
    var metadataProfileId: Int?
    /// Radarr only ("announced", "inCinemas", "released").
    var minimumAvailability: String?
    /// Sonarr only ("standard", "daily", "anime").
    var seriesType: String?
    /// Sonarr only.
    var seasonFolder: Bool?
    var rootFolderPath: String?
    /// Move the files into the new root folder (only with a root folder).
    var moveFiles = false
    /// Tag ids; nil leaves tags alone.
    var tags: [Int]?
    var tagMode: ArrTagMode = .add

    var isEmpty: Bool {
        monitored == nil && qualityProfileId == nil && metadataProfileId == nil && minimumAvailability == nil
            && seriesType == nil && seasonFolder == nil && rootFolderPath == nil && tags == nil
    }

    /// The editor key naming the selected ids ("movieIds", "seriesIds", "artistIds").
    static func idsKey(_ service: ArrService) -> String {
        switch service {
        case .sonarr: "seriesIds"
        case .lidarr: "artistIds"
        default: "movieIds"
        }
    }

    /// The body of `PUT {items}/editor`; settings that do not apply to the service are left out.
    func body(service: ArrService, ids: [Int]) -> JSONValue {
        var o: [String: JSONValue] = [Self.idsKey(service): .ints(ids)]
        if let monitored { o["monitored"] = .bool(monitored) }
        if let qualityProfileId { o["qualityProfileId"] = .number(qualityProfileId) }
        if service == .lidarr, let metadataProfileId { o["metadataProfileId"] = .number(metadataProfileId) }
        if service == .radarr, let minimumAvailability { o["minimumAvailability"] = .string(minimumAvailability) }
        if service == .sonarr, let seriesType { o["seriesType"] = .string(seriesType) }
        if service == .sonarr, let seasonFolder { o["seasonFolder"] = .bool(seasonFolder) }
        if let rootFolderPath, !rootFolderPath.isEmpty {
            o["rootFolderPath"] = .string(rootFolderPath)
            o["moveFiles"] = .bool(moveFiles)
        }
        if let tags {
            o["tags"] = .ints(tags)
            o["applyTags"] = .string(tagMode.rawValue)
        }
        return .object(o)
    }

    /// The body of `DELETE {items}/editor`.
    static func deleteBody(service: ArrService, ids: [Int], deleteFiles: Bool, addExclusion: Bool) -> JSONValue {
        [idsKey(service): .ints(ids), "deleteFiles": .bool(deleteFiles),
         ArrFlavor(service).exclusionParam: .bool(addExclusion)]
    }
}

/// What Seerr's Radarr/Sonarr "test" answers (`POST settings/{radarr|sonarr}/test`): the
/// server's profiles, root folders, language profiles (Sonarr v3) and tags.
struct SeerrServerTest: Equatable, Sendable {
    var profiles: [ArrSelectOption]
    var rootFolders: [ArrSelectOption]
    var languageProfiles: [ArrSelectOption]
    var tags: [ArrSelectOption]

    init(_ json: JSONValue) {
        profiles = json["profiles"].list.map { ArrSelectOption(value: $0["id"], name: $0["name"].string ?? $0["id"].text, hint: nil) }
        rootFolders = json["rootFolders"].list.compactMap { f in
            guard let path = f["path"].string, !path.isEmpty else { return nil }
            return ArrSelectOption(value: .string(path), name: path, hint: nil)
        }
        languageProfiles = json["languageProfiles"].list.map { ArrSelectOption(value: $0["id"], name: $0["name"].string ?? $0["id"].text, hint: nil) }
        tags = json["tags"].list.map { ArrSelectOption(value: $0["id"], name: $0["label"].string ?? $0["id"].text, hint: nil) }
    }

    /// Pickers for the server form, by key.
    var options: [String: [ArrSelectOption]] {
        var out: [String: [ArrSelectOption]] = [:]
        if !profiles.isEmpty {
            out["activeProfileId"] = profiles
            out["activeAnimeProfileId"] = profiles
        }
        if !rootFolders.isEmpty {
            out["activeDirectory"] = rootFolders
            out["activeAnimeDirectory"] = rootFolders
        }
        if !languageProfiles.isEmpty {
            out["activeLanguageProfileId"] = languageProfiles
            out["activeAnimeLanguageProfileId"] = languageProfiles
        }
        if !tags.isEmpty {
            out["tags"] = tags
            out["animeTags"] = tags
        }
        return out
    }

    /// Keeps `activeProfileName` (and the anime one) in step with the picked profile id.
    func syncingNames(_ server: JSONValue) -> JSONValue {
        var out = server
        for (idKey, nameKey) in [("activeProfileId", "activeProfileName"), ("activeAnimeProfileId", "activeAnimeProfileName")] {
            guard let o = out.object, o[nameKey] != nil || o[idKey] != nil else { continue }
            if let name = profiles.first(where: { $0.value == out[idKey] || ($0.value.int != nil && $0.value.int == out[idKey].int) })?.name {
                out[nameKey] = .string(name)
            }
        }
        return out
    }
}

/// Seerr's Radarr/Sonarr connection: the keys it needs, in the order the form shows them.
enum SeerrServerForm {
    static let connectionKeys = ["name", "hostname", "port", "useSsl", "baseUrl", "apiKey", "externalUrl"]

    static func defaultsKeys(_ kind: ArrService) -> [String] {
        var keys = ["activeProfileId", "activeDirectory"]
        if kind == .sonarr { keys += ["activeLanguageProfileId", "seriesType", "enableSeasonFolders"] }
        if kind == .radarr { keys += ["minimumAvailability"] }
        keys += ["tags", "is4k", "isDefault", "syncEnabled", "preventSearch", "tagRequests"]
        return keys
    }

    static let animeKeys = ["activeAnimeProfileId", "activeAnimeDirectory", "activeAnimeLanguageProfileId", "animeSeriesType", "animeTags"]

    /// Read-only or derived keys that are not edited by hand.
    static let hiddenKeys: Set<String> = ["id", "activeProfileName", "activeAnimeProfileName"]

    static func template(_ kind: ArrService) -> JSONValue {
        var t: JSONValue = ["name": "", "hostname": "", "port": .number(kind == .sonarr ? 8989 : 7878), "apiKey": "", "useSsl": false,
                            "baseUrl": "", "activeProfileId": 0, "activeProfileName": "", "activeDirectory": "", "is4k": false,
                            "isDefault": false, "externalUrl": "", "syncEnabled": false, "preventSearch": false, "tags": []]
        if kind == .radarr { t["minimumAvailability"] = "released" }
        if kind == .sonarr {
            t["seriesType"] = "standard"
            t["animeSeriesType"] = "anime"
            t["enableSeasonFolders"] = true
            t["activeAnimeProfileId"] = .null
            t["activeAnimeProfileName"] = .null
            t["activeAnimeDirectory"] = .null
            t["activeLanguageProfileId"] = .null
            t["activeAnimeLanguageProfileId"] = .null
            t["animeTags"] = []
        }
        return t
    }
}
