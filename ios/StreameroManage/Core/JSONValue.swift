import Foundation

/// Any JSON value. The *arr APIs are large and differ between versions, so Manage screens
/// read the fields they show and send everything else back untouched (D-048). Decoding
/// never fails on an unexpected shape.
enum JSONValue: Sendable, Hashable, Codable {
    case null
    case bool(Bool)
    case int(Int64)
    case double(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() {
            self = .null
        } else if let b = try? c.decode(Bool.self) {
            self = .bool(b)
        } else if let i = try? c.decode(Int64.self) {
            self = .int(i)
        } else if let d = try? c.decode(Double.self) {
            self = .double(d)
        } else if let s = try? c.decode(String.self) {
            self = .string(s)
        } else if let a = try? c.decode([JSONValue].self) {
            self = .array(a)
        } else if let o = try? c.decode([String: JSONValue].self) {
            self = .object(o)
        } else {
            throw DecodingError.dataCorruptedError(in: c, debugDescription: "Unsupported JSON value")
        }
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let b): try c.encode(b)
        case .int(let i): try c.encode(i)
        case .double(let d): try c.encode(d)
        case .string(let s): try c.encode(s)
        case .array(let a): try c.encode(a)
        case .object(let o): try c.encode(o)
        }
    }

    /// Parses a response body; an empty body is `null`.
    /// Parses a response body. JSONSerialization reads a whole library (thousands of movies)
    /// several times faster than decoding through `init(from:)`, which tries each type in turn
    /// and throws for every miss.
    static func parse(_ data: Data) throws -> JSONValue {
        if data.isEmpty { return .null }
        return JSONValue(foundation: try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]))
    }

    /// A value from JSONSerialization's object graph.
    init(foundation any: Any) {
        switch any {
        case let n as NSNumber:
            if n is NSDecimalNumber {
                // Integers beyond Int64 come back as decimals.
                self = .double(n.doubleValue)
            } else if CFGetTypeID(n) == CFBooleanGetTypeID() {
                self = .bool(n.boolValue)
            } else if CFNumberIsFloatType(n as CFNumber) {
                self = .double(n.doubleValue)
            } else {
                self = .int(n.int64Value)
            }
        case let s as String:
            self = .string(s)
        case let a as [Any]:
            self = .array(a.map(JSONValue.init(foundation:)))
        case let o as [String: Any]:
            self = .object(o.mapValues(JSONValue.init(foundation:)))
        default:
            self = .null
        }
    }

    func encoded() throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(self)
    }

    // MARK: Reading

    subscript(key: String) -> JSONValue {
        get {
            if case .object(let o) = self { return o[key] ?? .null }
            return .null
        }
        set {
            if case .object(var o) = self {
                o[key] = newValue
                self = .object(o)
            } else if case .null = self {
                self = .object([key: newValue])
            }
        }
    }

    subscript(index: Int) -> JSONValue {
        if case .array(let a) = self, a.indices.contains(index) { return a[index] }
        return .null
    }

    var isNull: Bool { if case .null = self { return true } else { return false } }

    var string: String? {
        if case .string(let s) = self { return s }
        return nil
    }

    /// A whole number (an integral double counts too).
    var int: Int? {
        switch self {
        case .int(let i): return Int(i)
        case .double(let d) where d.rounded() == d && abs(d) < 9e15: return Int(d)
        default: return nil
        }
    }

    var int64: Int64? {
        switch self {
        case .int(let i): return i
        case .double(let d) where d.rounded() == d && abs(d) < 9e15: return Int64(d)
        default: return nil
        }
    }

    var double: Double? {
        switch self {
        case .int(let i): return Double(i)
        case .double(let d): return d
        default: return nil
        }
    }

    var bool: Bool? {
        if case .bool(let b) = self { return b }
        return nil
    }

    var array: [JSONValue]? {
        if case .array(let a) = self { return a }
        return nil
    }

    var object: [String: JSONValue]? {
        if case .object(let o) = self { return o }
        return nil
    }

    /// The array, or an empty one.
    var list: [JSONValue] { array ?? [] }

    var isNumber: Bool {
        switch self {
        case .int, .double: return true
        default: return false
        }
    }

    /// Plain text for display: strings as they are, numbers and booleans spelled out.
    var text: String {
        switch self {
        case .null: return ""
        case .bool(let b): return b ? "true" : "false"
        case .int(let i): return String(i)
        case .double(let d):
            if d.rounded() == d && abs(d) < 1e15 { return String(Int64(d)) }
            return String(d)
        case .string(let s): return s
        case .array(let a): return a.map(\.text).joined(separator: ", ")
        case .object:
            return (try? encoded()).map { String(decoding: $0, as: UTF8.self) } ?? ""
        }
    }

    /// A stable identifier for list rows: the `id` field, else the value's own hash.
    var rowID: String {
        let id = self["id"]
        if !id.isNull { return id.text }
        return String(hashValue)
    }

    /// The first non-empty string among keys (titles differ between services).
    func firstString(_ keys: String...) -> String? {
        for key in keys {
            if let s = self[key].string, !s.isEmpty { return s }
        }
        return nil
    }

    // MARK: Paths

    /// Reads a dotted path ("options.webhookUrl").
    func value(at path: String) -> JSONValue {
        var current = self
        for part in path.split(separator: ".") { current = current[String(part)] }
        return current
    }

    /// Writes a dotted path, creating objects on the way.
    mutating func setValue(_ value: JSONValue, at path: String) {
        let parts = path.split(separator: ".").map(String.init)
        self = Self.setting(self, parts[...], value)
    }

    private static func setting(_ base: JSONValue, _ parts: ArraySlice<String>, _ value: JSONValue) -> JSONValue {
        guard let first = parts.first else { return value }
        var object = base.object ?? [:]
        object[first] = setting(object[first] ?? .null, parts.dropFirst(), value)
        return .object(object)
    }

    /// Leaf values by dotted path; nested objects are walked, arrays are leaves. Sorted by path.
    func flattened(prefix: String = "") -> [JSONPathValue] {
        guard case .object(let o) = self else { return prefix.isEmpty ? [] : [JSONPathValue(path: prefix, value: self)] }
        var out: [JSONPathValue] = []
        for key in o.keys.sorted() {
            let path = prefix.isEmpty ? key : prefix + "." + key
            let child = o[key] ?? .null
            if case .object(let inner) = child, !inner.isEmpty {
                out += child.flattened(prefix: path)
            } else {
                out.append(JSONPathValue(path: path, value: child))
            }
        }
        return out
    }
}

/// A leaf of a JSON object by dotted path.
struct JSONPathValue: Hashable, Sendable {
    var path: String
    var value: JSONValue
}

extension JSONValue: ExpressibleByStringLiteral, ExpressibleByIntegerLiteral, ExpressibleByBooleanLiteral,
    ExpressibleByArrayLiteral, ExpressibleByDictionaryLiteral, ExpressibleByFloatLiteral {
    init(stringLiteral value: String) { self = .string(value) }
    init(integerLiteral value: Int) { self = .int(Int64(value)) }
    init(booleanLiteral value: Bool) { self = .bool(value) }
    init(arrayLiteral elements: JSONValue...) { self = .array(elements) }
    init(dictionaryLiteral elements: (String, JSONValue)...) {
        var o: [String: JSONValue] = [:]
        for (k, v) in elements { o[k] = v }
        self = .object(o)
    }
    init(floatLiteral value: Double) { self = .double(value) }

    static func number(_ i: Int) -> JSONValue { .int(Int64(i)) }
    static func ints(_ values: [Int]) -> JSONValue { .array(values.map { .int(Int64($0)) }) }
}

extension String {
    /// "enableAutomaticSearch" → "Enable Automatic Search" (API keys shown as the API names them).
    var humanizedKey: String {
        let last = split(separator: ".").last.map(String.init) ?? self
        var out = ""
        var previous: Character?
        for ch in last {
            if ch == "_" || ch == "-" {
                out.append(" ")
            } else if ch.isUppercase, let p = previous, p.isLowercase || p.isNumber {
                out.append(" ")
                out.append(ch)
            } else {
                out.append(ch)
            }
            previous = ch
        }
        guard let first = out.first else { return out }
        return first.uppercased() + out.dropFirst()
    }
}
