import Foundation

/// One choice of a schema `select` field.
struct ArrSelectOption: Hashable, Sendable, Identifiable {
    var value: JSONValue
    var name: String
    var hint: String?

    var id: String { value.text }
}

/// A field of a Servarr provider schema (`fields[]` of indexers, download clients, import
/// lists, notifications, metadata, Prowlarr applications, custom format specifications).
/// Field types the app does not know decode as `.unknown` and never break the form.
struct ArrField: Hashable, Sendable, Identifiable {
    enum Kind: Hashable, Sendable {
        case textbox, number, password, checkbox, select, path, filePath, url, textArea
        case tag, tagSelect, info, hidden, action, captcha, oAuth, device, keyValueList, playlist
        case unknown(String)

        init(_ raw: String?) {
            switch (raw ?? "textbox").lowercased() {
            case "textbox": self = .textbox
            case "number": self = .number
            case "password": self = .password
            case "checkbox": self = .checkbox
            case "select": self = .select
            case "path": self = .path
            case "filepath": self = .filePath
            case "url": self = .url
            case "textarea": self = .textArea
            case "tag": self = .tag
            case "tagselect": self = .tagSelect
            case "info": self = .info
            case "hidden": self = .hidden
            case "action": self = .action
            case "captcha": self = .captcha
            case "oauth": self = .oAuth
            case "device": self = .device
            case "keyvaluelist": self = .keyValueList
            case "playlist": self = .playlist
            default: self = .unknown(raw ?? "")
            }
        }
    }

    var name: String
    var label: String
    var kind: Kind
    var value: JSONValue
    var helpText: String?
    var helpLink: String?
    var advanced: Bool
    var privacy: String?
    var hidden: String?
    var unit: String?
    var placeholder: String?
    var section: String?
    var isFloat: Bool
    var options: [ArrSelectOption]
    /// Options loaded from the service (`POST {resource}/action/{action}`).
    var optionsAction: String?
    /// The field as the API sent it; saved back with only `value` replaced.
    var raw: JSONValue

    var id: String { name }

    init(json: JSONValue) {
        raw = json
        name = json["name"].string ?? ""
        label = json["label"].string?.nilIfBlank ?? (json["name"].string ?? "").humanizedKey
        kind = Kind(json["type"].string)
        value = json["value"]
        helpText = json["helpText"].string?.nilIfBlank ?? json["helpTextWarning"].string?.nilIfBlank
        helpLink = json["helpLink"].string?.nilIfBlank
        advanced = json["advanced"].bool ?? false
        privacy = json["privacy"].string
        hidden = json["hidden"].string
        unit = json["unit"].string?.nilIfBlank
        placeholder = json["placeholder"].string?.nilIfBlank
        section = json["section"].string?.nilIfBlank
        isFloat = json["isFloat"].bool ?? false
        optionsAction = json["selectOptionsProviderAction"].string?.nilIfBlank
        options = json["selectOptions"].list.map { o in
            ArrSelectOption(value: o["value"], name: o["name"].string ?? o["value"].text, hint: o["hint"].string?.nilIfBlank)
        }
    }

    /// Passwords and API keys come back masked; they are write-only in the app.
    var isSecret: Bool {
        kind == .password || privacy?.lowercased() == "password" || privacy?.lowercased() == "apikey"
    }

    /// Hidden fields are kept and sent back but never shown.
    var isVisible: Bool {
        if kind == .hidden { return false }
        switch hidden?.lowercased() {
        case "hidden": return false
        case "hiddenifnotset": return !(value.isNull || value.text.isEmpty)
        default: return true
        }
    }

    /// A select whose value is a list picks several options.
    var isMultiSelect: Bool { (kind == .select || kind == .tagSelect) && (value.array != nil || kind == .tagSelect) }
}

/// The editable values of a schema form. Secrets are write-only: the field starts empty in
/// the UI and an unchanged secret goes back exactly as the API sent it (Servarr keeps the
/// stored value for its own mask), never as the empty text the user saw.
struct SchemaFormState: Equatable, Sendable {
    private(set) var fields: [ArrField]
    private var values: [String: JSONValue]
    /// Secrets the user typed, by field name.
    private var secrets: [String: String] = [:]

    init(fields: [ArrField]) {
        self.fields = fields
        var v: [String: JSONValue] = [:]
        for f in fields { v[f.name] = f.value }
        values = v
    }

    init(json: JSONValue) {
        self.init(fields: json.list.map(ArrField.init(json:)))
    }

    func value(_ name: String) -> JSONValue { values[name] ?? .null }

    mutating func set(_ name: String, _ value: JSONValue) { values[name] = value }

    /// What the secret field shows: only what the user typed in this session.
    func secretText(_ name: String) -> String { secrets[name] ?? "" }

    mutating func setSecret(_ name: String, _ text: String) {
        secrets[name] = text.isEmpty ? nil : text
    }

    /// Fields whose value differs from what the API sent.
    var changedNames: Set<String> {
        Set(fields.filter { f in
            f.isSecret ? secrets[f.name] != nil : value(f.name) != f.value
        }.map(\.name))
    }

    /// The `fields` array to send back: every field as received, with its current value.
    func outgoingFields() -> JSONValue {
        .array(fields.map { f in
            var raw = f.raw
            if f.isSecret {
                raw["value"] = secrets[f.name].map { JSONValue.string($0) } ?? f.value
            } else {
                raw["value"] = value(f.name)
            }
            return raw
        })
    }

    /// Replaces a field's options (loaded with `selectOptionsProviderAction`).
    mutating func setOptions(_ name: String, _ options: [ArrSelectOption]) {
        if let i = fields.firstIndex(where: { $0.name == name }) { fields[i].options = options }
    }
}

/// A generic editable JSON object (config endpoints, Bazarr and Seerr settings). Values are
/// addressed by dotted path; keys that look like secrets are write-only like schema secrets.
struct ObjectFormState: Equatable, Sendable {
    let original: JSONValue
    private(set) var current: JSONValue
    private var secrets: [String: String] = [:]

    init(_ object: JSONValue) {
        original = object
        current = object
    }

    static func isSecretKey(_ path: String) -> Bool {
        let key = (path.split(separator: ".").last.map(String.init) ?? path).lowercased()
        if key.hasSuffix("confirmation") { return true }
        return key.contains("password") || key.contains("apikey") || key == "api_key" || key.contains("secret")
            || key.hasSuffix("token") || key.hasSuffix("_key") || key == "passkey"
    }

    func value(_ path: String) -> JSONValue { current.value(at: path) }

    mutating func set(_ path: String, _ value: JSONValue) { current.setValue(value, at: path) }

    func secretText(_ path: String) -> String { secrets[path] ?? "" }

    mutating func setSecret(_ path: String, _ text: String) {
        secrets[path] = text.isEmpty ? nil : text
    }

    /// The whole object to save (PUT): unchanged secrets keep the API's value.
    func fullObject() -> JSONValue {
        var out = current
        for (path, text) in secrets { out.setValue(.string(text), at: path) }
        return out
    }

    /// Only what changed, by path (for APIs that take partial updates, like Bazarr's settings):
    /// unchanged secrets are not sent at all.
    func changes() -> [JSONPathValue] {
        let before = Dictionary(original.flattened().map { ($0.path, $0.value) }, uniquingKeysWith: { a, _ in a })
        var out: [JSONPathValue] = []
        for item in current.flattened() where !Self.isSecretKey(item.path) {
            if before[item.path] != item.value { out.append(item) }
        }
        for (path, text) in secrets.sorted(by: { $0.key < $1.key }) { out.append(JSONPathValue(path: path, value: .string(text))) }
        return out
    }

    var hasChanges: Bool { !changes().isEmpty }
}
