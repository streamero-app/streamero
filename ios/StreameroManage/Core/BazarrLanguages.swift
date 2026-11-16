import Foundation

/// One subtitle language Bazarr knows (`GET api/system/languages`: `{name, code2, code3, enabled}`).
struct BazarrLanguage: Hashable, Sendable, Identifiable {
    var code2: String
    var code3: String
    var name: String
    var enabled: Bool

    var id: String { code2 }

    init(json: JSONValue) {
        code2 = json["code2"].text
        code3 = json["code3"].text
        name = json["name"].string?.nilIfBlank ?? json["code2"].text
        enabled = BazarrFlag.isOn(json["enabled"])
    }

    init(code2: String, code3: String = "", name: String, enabled: Bool) {
        self.code2 = code2
        self.code3 = code3
        self.name = name
        self.enabled = enabled
    }
}

/// Bazarr writes some flags as the strings "True"/"False" and others as booleans. Edits keep
/// whichever form the value already had, so a profile goes back the way it came.
enum BazarrFlag {
    static func isOn(_ value: JSONValue) -> Bool {
        if let b = value.bool { return b }
        if let i = value.int { return i != 0 }
        return value.string?.lowercased() == "true"
    }

    /// The new value in the shape of `previous` ("True"/"False" unless it was a boolean).
    static func value(_ on: Bool, like previous: JSONValue) -> JSONValue {
        if previous.bool != nil { return .bool(on) }
        return .string(on ? "True" : "False")
    }
}

/// What a subtitle in a profile item must be.
enum BazarrSubtitleKind: String, CaseIterable, Identifiable, Sendable {
    case normal, hearingImpaired, forced
    var id: String { rawValue }
}

/// A language profile (`GET api/system/languages/profiles`): `{profileId, name, cutoff,
/// items: [{id, language, forced, hi, audio_exclude}], mustContain, mustNotContain,
/// originalFormat, tag}`. Keys the app does not know stay in `raw` and are saved back.
struct BazarrLanguageProfile: Hashable, Sendable, Identifiable {
    var raw: JSONValue

    init(_ raw: JSONValue) { self.raw = raw }

    var id: Int { raw["profileId"].int ?? -1 }

    var name: String {
        get { raw["name"].text }
        set { raw["name"] = .string(newValue) }
    }

    var items: [JSONValue] {
        get { raw["items"].list }
        set { raw["items"] = .array(newValue) }
    }

    /// The item id subtitles stop upgrading at; nil when there is none.
    var cutoff: Int? {
        get { raw["cutoff"].int }
        set { raw["cutoff"] = newValue.map { .number($0) } ?? .null }
    }

    var mustContain: [String] {
        get { raw["mustContain"].list.map(\.text) }
        set { raw["mustContain"] = .array(newValue.map { .string($0) }) }
    }

    var mustNotContain: [String] {
        get { raw["mustNotContain"].list.map(\.text) }
        set { raw["mustNotContain"] = .array(newValue.map { .string($0) }) }
    }

    var originalFormat: Bool {
        get { BazarrFlag.isOn(raw["originalFormat"]) }
        set { raw["originalFormat"] = raw["originalFormat"].string != nil ? BazarrFlag.value(newValue, like: raw["originalFormat"]) : .bool(newValue) }
    }

    /// The tag that assigns the profile automatically (Bazarr's own tag name, free text).
    var tag: String {
        get { raw["tag"].text }
        set { raw["tag"] = newValue.isEmpty ? .null : .string(newValue) }
    }

    /// A new, empty profile with an id after the given ones.
    static func new(name: String, after profiles: [BazarrLanguageProfile]) -> BazarrLanguageProfile {
        let next = (profiles.map(\.id).max() ?? 0) + 1
        return BazarrLanguageProfile([
            "profileId": .number(next), "name": .string(name), "cutoff": .null, "items": [],
            "mustContain": [], "mustNotContain": [], "originalFormat": false, "tag": .null,
        ])
    }

    /// Adds a language item; flags take the form the profile's other items use.
    mutating func addItem(language: String) {
        let sample = items.first ?? .null
        let next = (items.compactMap { $0["id"].int }.max() ?? 0) + 1
        var item: JSONValue = ["id": .number(next), "language": .string(language)]
        for key in ["forced", "hi", "audio_exclude"] {
            item[key] = BazarrFlag.value(false, like: sample[key])
        }
        // Newer versions add more flags; keep them present and off.
        for (key, value) in sample.object ?? [:] where item[key].isNull && key != "id" && key != "language" {
            item[key] = value.bool != nil || value.string?.lowercased() == "true" || value.string?.lowercased() == "false"
                ? BazarrFlag.value(false, like: value) : value
        }
        items.append(item)
    }

    mutating func removeItems(at offsets: IndexSet) {
        var list = items
        let removed = offsets.compactMap { list.indices.contains($0) ? list[$0]["id"].int : nil }
        for i in offsets.sorted(by: >) where list.indices.contains(i) { list.remove(at: i) }
        items = list
        if let c = cutoff, removed.contains(c) { cutoff = nil }
    }

    mutating func moveItems(from source: IndexSet, to destination: Int) {
        var list = items
        let moving = source.sorted().map { list[$0] }
        for i in source.sorted(by: >) { list.remove(at: i) }
        let target = destination - source.filter { $0 < destination }.count
        list.insert(contentsOf: moving, at: max(0, min(target, list.count)))
        items = list
    }

    static func kind(of item: JSONValue) -> BazarrSubtitleKind {
        if BazarrFlag.isOn(item["forced"]) { return .forced }
        if BazarrFlag.isOn(item["hi"]) { return .hearingImpaired }
        return .normal
    }

    static func setKind(_ kind: BazarrSubtitleKind, on item: inout JSONValue) {
        item["forced"] = BazarrFlag.value(kind == .forced, like: item["forced"])
        item["hi"] = BazarrFlag.value(kind == .hearingImpaired, like: item["hi"])
    }
}

/// Bazarr saves languages with its settings form (`POST api/system/settings`):
/// `languages-enabled` once per enabled language code, `languages-profiles` as one JSON text
/// with every profile.
enum BazarrLanguageSettings {
    static func formFields(enabled: [String]?, profiles: [BazarrLanguageProfile]?) -> [(String, String)] {
        var out: [(String, String)] = []
        if let enabled {
            for code in enabled where !code.isEmpty { out.append(("languages-enabled", code)) }
        }
        if let profiles {
            let json = JSONValue.array(profiles.map(\.raw))
            let text = (try? json.encoded()).map { String(decoding: $0, as: UTF8.self) } ?? "[]"
            out.append(("languages-profiles", text))
        }
        return out
    }

    /// Enabled codes in the order of the full language list (stable for comparing).
    static func enabledCodes(_ languages: [BazarrLanguage]) -> [String] {
        languages.filter(\.enabled).map(\.code2)
    }
}
