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

    /// Whether a plain-http address leaves the local network. The app allows plain http (ATS
    /// arbitrary loads, D-057) because home servers on the LAN rarely have certificates; for an
    /// address on the internet the user has to confirm it first, since the password, the
    /// session token and everything watched would cross the internet unencrypted.
    static func isPlainHTTPOverInternet(_ url: URL) -> Bool {
        guard url.scheme?.lowercased() == "http", let host = url.host() else { return false }
        return !isLocal(host: host)
    }

    /// Loopback, private and link-local addresses, CGNAT (Tailscale and similar), `.local`,
    /// `.home.arpa`, `.lan`, `.internal` and single-label names.
    static func isLocal(host raw: String) -> Bool {
        let host = raw.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if host == "localhost" || !host.contains(".") && !host.contains(":") { return true }
        for suffix in [".local", ".localhost", ".home.arpa", ".lan", ".internal"] where host.hasSuffix(suffix) { return true }
        if host.contains(":") {
            // IPv6: loopback, unique local (fc00::/7) and link-local (fe80::/10).
            if host == "::1" { return true }
            guard let first = host.split(separator: ":", omittingEmptySubsequences: false).first,
                  let word = UInt16(first, radix: 16) else { return false }
            return (word & 0xfe00) == 0xfc00 || (word & 0xffc0) == 0xfe80
        }
        let parts = host.split(separator: ".").compactMap { UInt8($0) }
        guard parts.count == 4 else { return false }
        switch (parts[0], parts[1]) {
        case (10, _), (127, _): return true
        case (172, 16...31): return true
        case (192, 168): return true
        case (169, 254): return true
        case (100, 64...127): return true
        default: return false
        }
    }
}
