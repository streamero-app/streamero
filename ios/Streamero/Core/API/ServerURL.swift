import Foundation

/// Normalizes user-typed server addresses ("192.168.1.10:8787", "demo.example.net") into base URLs.
enum ServerURL {
    static func normalize(_ input: String) -> URL? {
        var text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        if !text.lowercased().hasPrefix("http://") && !text.lowercased().hasPrefix("https://") {
            text = "https://" + text
        }
        while text.hasSuffix("/") { text.removeLast() }
        guard var comps = URLComponents(string: text), let host = comps.host, !host.isEmpty else { return nil }
        if comps.path.hasSuffix("/api/v1") {
            comps.path = String(comps.path.dropLast("/api/v1".count))
        }
        return comps.url
    }

    /// The API base for a server base URL.
    static func apiBase(_ base: URL) -> URL {
        base.appending(path: "api/v1")
    }
}
