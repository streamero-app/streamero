import Foundation

/// A quality (or a group of qualities) in a quality profile.
struct QualityProfileItem: Hashable, Sendable, Identifiable {
    /// The quality's id, or the group's id (Servarr numbers groups from 1000).
    var id: Int
    var name: String
    var allowed: Bool
    var isGroup: Bool
    /// Names of the qualities in a group.
    var members: [String]
    var raw: JSONValue

    init(json: JSONValue) {
        raw = json
        let members = json["items"].list
        if members.isEmpty, !json["quality"].isNull {
            id = json["quality"]["id"].int ?? 0
            name = json["quality"]["name"].string ?? ""
            isGroup = false
            self.members = []
        } else {
            id = json["id"].int ?? json["quality"]["id"].int ?? 0
            name = json["name"].string ?? json["quality"]["name"].string ?? ""
            isGroup = true
            self.members = members.map { $0["quality"]["name"].string ?? $0["name"].string ?? "" }
        }
        allowed = json["allowed"].bool ?? false
    }

    var json: JSONValue {
        var out = raw
        out["allowed"] = .bool(allowed)
        if isGroup {
            // A group's qualities follow the group.
            out["items"] = .array(raw["items"].list.map { m in
                var m = m
                m["allowed"] = .bool(allowed)
                return m
            })
        }
        return out
    }
}

/// Editing a quality profile. The API lists qualities from lowest to highest; the editor
/// shows them highest first (most wanted on top), and turns the order back when saving.
struct QualityProfileModel: Equatable, Sendable {
    var raw: JSONValue
    var name: String
    var upgradeAllowed: Bool
    var cutoff: Int
    /// Highest quality first.
    var items: [QualityProfileItem]
    var formatScores: [FormatScore]
    var minFormatScore: Int
    var cutoffFormatScore: Int
    var minUpgradeFormatScore: Int?

    struct FormatScore: Hashable, Sendable, Identifiable {
        var id: Int
        var name: String
        var score: Int
    }

    init(json: JSONValue) {
        raw = json
        name = json["name"].string ?? ""
        upgradeAllowed = json["upgradeAllowed"].bool ?? false
        cutoff = json["cutoff"].int ?? 0
        items = json["items"].list.map(QualityProfileItem.init(json:)).reversed()
        formatScores = json["formatItems"].list.map {
            FormatScore(id: $0["format"].int ?? 0, name: $0["name"].string ?? "", score: $0["score"].int ?? 0)
        }
        minFormatScore = json["minFormatScore"].int ?? 0
        cutoffFormatScore = json["cutoffFormatScore"].int ?? 0
        minUpgradeFormatScore = json["minUpgradeFormatScore"].int
    }

    /// Only allowed qualities can be the cutoff.
    var allowedItems: [QualityProfileItem] { items.filter(\.allowed) }

    /// Moves rows in display order (as `List.onMove` reports them).
    mutating func move(fromOffsets source: IndexSet, toOffset destination: Int) {
        let moving = source.sorted().map { items[$0] }
        var rest = items.enumerated().filter { !source.contains($0.offset) }.map(\.element)
        let insertAt = destination - source.filter { $0 < destination }.count
        rest.insert(contentsOf: moving, at: max(0, min(insertAt, rest.count)))
        items = rest
    }

    mutating func setAllowed(_ id: Int, _ allowed: Bool) {
        guard let i = items.firstIndex(where: { $0.id == id }) else { return }
        items[i].allowed = allowed
        fixCutoff()
    }

    /// Keeps the cutoff on an allowed quality (the highest one when it was turned off).
    mutating func fixCutoff() {
        if !allowedItems.contains(where: { $0.id == cutoff }), let first = allowedItems.first {
            cutoff = first.id
        }
    }

    mutating func setScore(format id: Int, _ score: Int) {
        if let i = formatScores.firstIndex(where: { $0.id == id }) { formatScores[i].score = score }
    }

    /// The profile to PUT back, in the API's order (lowest first).
    func json() -> JSONValue {
        var out = raw
        out["name"] = .string(name)
        out["upgradeAllowed"] = .bool(upgradeAllowed)
        out["cutoff"] = .number(cutoff)
        out["items"] = .array(items.reversed().map(\.json))
        if !raw["formatItems"].isNull {
            out["formatItems"] = .array(formatScores.map { fs -> JSONValue in
                .object(["format": .number(fs.id), "name": .string(fs.name), "score": .number(fs.score)])
            })
            out["minFormatScore"] = .number(minFormatScore)
            out["cutoffFormatScore"] = .number(cutoffFormatScore)
            if let minUpgradeFormatScore { out["minUpgradeFormatScore"] = .number(minUpgradeFormatScore) }
        }
        return out
    }
}
